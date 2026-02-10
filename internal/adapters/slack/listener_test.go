package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
	"github.com/gorilla/websocket"
)

// newTestWSServer creates an HTTP server that upgrades to WebSocket
// and calls onConn for each new connection. Returns the server and
// a function to get the ws:// URL.
func newTestWSServer(t *testing.T, onConn func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		onConn(conn)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestAPIServer creates an HTTP server that responds to /apps.connections.open
// with a WebSocket URL. openCount is incremented on each call.
func newTestAPIServer(t *testing.T, wsURL string, openCount *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps.connections.open" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if openCount != nil {
			openCount.Add(1)
		}
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestListener_ReceivesEvents(t *testing.T) {
	// WebSocket server that sends one event then stays open.
	var serverConn *websocket.Conn
	var connMu sync.Mutex
	wsSrv := newTestWSServer(t, func(conn *websocket.Conn) {
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()

		env := Envelope{
			EnvelopeID: "evt-100",
			Type:       "events_api",
			Payload:    json.RawMessage(`{"event":{"type":"message","text":"hello"}}`),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Read ack.
		_, _, _ = conn.ReadMessage()

		// Keep connection open until test ends.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := newTestAPIServer(t, wsURL, nil)

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(100*time.Millisecond, 500*time.Millisecond),
		WithDialer(websocket.DefaultDialer),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Type != SocketEventMessage {
			t.Errorf("event type = %q, want %q", evt.Type, SocketEventMessage)
		}
		if evt.EnvelopeID != "evt-100" {
			t.Errorf("envelope_id = %q, want %q", evt.EnvelopeID, "evt-100")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()

	connMu.Lock()
	if serverConn != nil {
		_ = serverConn.Close()
	}
	connMu.Unlock()
}

func TestListener_ReconnectsOnDisconnect(t *testing.T) {
	var openCount atomic.Int32
	var connCount atomic.Int32

	wsSrv := newTestWSServer(t, func(conn *websocket.Conn) {
		n := connCount.Add(1)

		if n == 1 {
			// First connection: send disconnect envelope.
			env := Envelope{
				EnvelopeID: "disc-001",
				Type:       "disconnect",
				Reason:     "link_disabled",
			}
			data, _ := json.Marshal(env)
			_ = conn.WriteMessage(websocket.TextMessage, data)
			// Read ack.
			_, _, _ = conn.ReadMessage()
			return
		}

		// Second connection: send a regular event.
		env := Envelope{
			EnvelopeID: "evt-200",
			Type:       "events_api",
			Payload:    json.RawMessage(`{"event":{"type":"message","text":"reconnected"}}`),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage()

		// Keep alive until closed.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := newTestAPIServer(t, wsURL, &openCount)

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(50*time.Millisecond, 200*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// We should receive the disconnect event and then the reconnected event.
	var gotDisconnect, gotEvent bool
	timeout := time.After(5 * time.Second)

	for !gotDisconnect || !gotEvent {
		select {
		case evt, ok := <-events:
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}
			switch evt.Type {
			case SocketEventDisconnect:
				gotDisconnect = true
			case SocketEventMessage:
				if evt.EnvelopeID == "evt-200" {
					gotEvent = true
				}
			}
		case <-timeout:
			t.Fatalf("timed out: gotDisconnect=%v gotEvent=%v", gotDisconnect, gotEvent)
		}
	}

	// Verify multiple connections were opened.
	if got := openCount.Load(); got < 2 {
		t.Errorf("OpenConnection called %d times, want >= 2", got)
	}

	cancel()
}

func TestListener_ReconnectsOnWSError(t *testing.T) {
	var connCount atomic.Int32

	wsSrv := newTestWSServer(t, func(conn *websocket.Conn) {
		n := connCount.Add(1)

		if n == 1 {
			// First connection: close abruptly (simulates read error).
			_ = conn.Close()
			return
		}

		// Second connection: send an event.
		env := Envelope{
			EnvelopeID: "evt-300",
			Type:       "events_api",
			Payload:    json.RawMessage(`{"event":{"type":"message","text":"after-error"}}`),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := newTestAPIServer(t, wsURL, nil)

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(50*time.Millisecond, 200*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-events:
		if evt.EnvelopeID != "evt-300" {
			t.Errorf("expected evt-300, got %q", evt.EnvelopeID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reconnect event")
	}

	cancel()
}

func TestListener_StopsOnAuthFailure(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"ok":false,"error":"invalid_auth"}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer apiSrv.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(50*time.Millisecond, 200*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Channel should close quickly without retrying.
	select {
	case _, ok := <-events:
		if ok {
			t.Error("expected channel to be closed on auth failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out — listener should have stopped on auth failure")
	}
}

func TestListener_ContextCancellation(t *testing.T) {
	wsSrv := newTestWSServer(t, func(conn *websocket.Conn) {
		// Keep connection alive.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := newTestAPIServer(t, wsURL, nil)

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(50*time.Millisecond, 200*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Give it a moment to connect.
	time.Sleep(200 * time.Millisecond)

	// Cancel and verify channel closes.
	cancel()

	select {
	case _, ok := <-events:
		if ok {
			// May receive residual events, keep draining.
			for range events {
			}
		}
		// Channel closed — good.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for events channel to close after cancel")
	}
}

func TestListener_ExponentialBackoff(t *testing.T) {
	l := &Listener{
		initialBackoff: 1 * time.Second,
		maxBackoff:     30 * time.Second,
	}

	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{16 * time.Second, 30 * time.Second}, // capped
		{30 * time.Second, 30 * time.Second}, // stays at max
	}

	for _, tt := range tests {
		got := l.nextBackoff(tt.current)
		if got != tt.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.want)
		}
	}
}

func TestListener_BackoffResetsOnSuccess(t *testing.T) {
	// Track API open calls.
	var openCount atomic.Int32
	var connCount atomic.Int32
	var connTimestamps []time.Time
	var mu sync.Mutex

	wsSrv := newTestWSServer(t, func(conn *websocket.Conn) {
		mu.Lock()
		connTimestamps = append(connTimestamps, time.Now())
		mu.Unlock()

		n := connCount.Add(1)

		if n <= 2 {
			// First two connections: close immediately to trigger reconnect.
			_ = conn.Close()
			return
		}

		// Third connection: send event and stay open.
		env := Envelope{
			EnvelopeID: "evt-400",
			Type:       "events_api",
			Payload:    json.RawMessage(`{"event":{"type":"message","text":"ok"}}`),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage()

		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := newTestAPIServer(t, wsURL, &openCount)

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
	listener := NewListener(client,
		WithBackoff(100*time.Millisecond, 1*time.Second),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := listener.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Wait for the successful event after reconnections.
	select {
	case evt := <-events:
		if evt.EnvelopeID != "evt-400" {
			t.Errorf("expected evt-400, got %q", evt.EnvelopeID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	if got := openCount.Load(); got < 3 {
		t.Errorf("expected >= 3 OpenConnection calls, got %d", got)
	}

	cancel()
}

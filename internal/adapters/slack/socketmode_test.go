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

func TestOpenConnection(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		response   string
		wantURL    string
		wantErr    bool
		errContain string
	}{
		{
			name:       "successful connection returns WSS URL",
			statusCode: http.StatusOK,
			response:   `{"ok":true,"url":"wss://wss-primary.slack.com/link/?ticket=abc123"}`,
			wantURL:    "wss://wss-primary.slack.com/link/?ticket=abc123",
		},
		{
			name:       "invalid auth error",
			statusCode: http.StatusOK,
			response:   `{"ok":false,"error":"invalid_auth"}`,
			wantErr:    true,
			errContain: "authentication failed",
		},
		{
			name:       "non-auth API error",
			statusCode: http.StatusOK,
			response:   `{"ok":false,"error":"too_many_websockets"}`,
			wantErr:    true,
			errContain: "too_many_websockets",
		},
		{
			name:       "HTTP 500 error",
			statusCode: http.StatusInternalServerError,
			response:   `Internal Server Error`,
			wantErr:    true,
			errContain: "HTTP 500",
		},
		{
			name:       "empty URL in response",
			statusCode: http.StatusOK,
			response:   `{"ok":true,"url":""}`,
			wantErr:    true,
			errContain: "empty WebSocket URL",
		},
		{
			name:       "malformed JSON response",
			statusCode: http.StatusOK,
			response:   `{not json`,
			wantErr:    true,
			errContain: "failed to parse response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/apps.connections.open") {
					t.Errorf("path = %s, want /apps.connections.open", r.URL.Path)
				}
				auth := r.Header.Get("Authorization")
				if auth != "Bearer "+testutil.FakeSlackAppToken {
					t.Errorf("auth = %q, want Bearer %s", auth, testutil.FakeSlackAppToken)
				}
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, server.URL)
			url, err := client.OpenConnection(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.errContain)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestOpenConnection_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, "http://localhost:1")
	_, err := client.OpenConnection(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// TestListen_ReceivesEvents verifies that Listen connects, receives events,
// and acknowledges envelopes.
func TestListen_ReceivesEvents(t *testing.T) {
	var wsConns sync.WaitGroup

	// Mock apps.connections.open → returns WSS URL pointing at our mock WS server.
	wsMux := http.NewServeMux()
	upgrader := websocket.Upgrader{}

	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		wsConns.Done()

		// Send a message event.
		env := socketEnvelope{
			EnvelopeID: "e1",
			Type:       EnvelopeTypeEventsAPI,
			Payload: mustMarshal(eventsAPIPayload{
				Type: "event_callback",
				Event: mustMarshal(innerEvent{
					Type:    EventTypeMessage,
					Channel: "C123",
					User:    "U456",
					Text:    "hello pilot",
					TS:      "1234567890.123456",
				}),
			}),
		}
		data, _ := json.Marshal(env)
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return
		}

		// Read ack.
		_, ackData, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ack struct {
			EnvelopeID string `json:"envelope_id"`
		}
		if err := json.Unmarshal(ackData, &ack); err != nil {
			t.Errorf("unmarshal ack: %v", err)
			return
		}
		if ack.EnvelopeID != "e1" {
			t.Errorf("ack envelope_id = %q, want %q", ack.EnvelopeID, "e1")
		}

		// Keep connection alive until test completes.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		data, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer apiServer.Close()

	wsConns.Add(1) // expect one connection

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	wsConns.Wait() // wait for WS connection

	select {
	case evt := <-ch:
		if evt == nil {
			t.Fatal("received nil event")
		}
		if evt.Type != EventTypeMessage {
			t.Errorf("event type = %q, want %q", evt.Type, EventTypeMessage)
		}
		if evt.Text != "hello pilot" {
			t.Errorf("event text = %q, want %q", evt.Text, "hello pilot")
		}
		if evt.ChannelID != "C123" {
			t.Errorf("channel = %q, want %q", evt.ChannelID, "C123")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

// TestListen_ReconnectsOnDisconnect verifies the reconnect logic when
// a disconnect envelope is received.
func TestListen_ReconnectsOnDisconnect(t *testing.T) {
	var connectCount atomic.Int32

	upgrader := websocket.Upgrader{}

	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		n := connectCount.Add(1)

		if n == 1 {
			// First connection: send disconnect envelope.
			env := socketEnvelope{
				EnvelopeID: "disc-1",
				Type:       EnvelopeTypeDisconnect,
				Reason:     "link_disabled",
			}
			data, _ := json.Marshal(env)
			_ = conn.WriteMessage(websocket.TextMessage, data)
			// Read ack, then connection will close.
			_, _, _ = conn.ReadMessage()
			return
		}

		// Second connection: send a real event.
		env := socketEnvelope{
			EnvelopeID: "e2",
			Type:       EnvelopeTypeEventsAPI,
			Payload: mustMarshal(eventsAPIPayload{
				Type: "event_callback",
				Event: mustMarshal(innerEvent{
					Type:    EventTypeMessage,
					Channel: "C999",
					User:    "U111",
					Text:    "reconnected",
					TS:      "9999999999.999999",
				}),
			}),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		// Keep alive.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		data, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer apiServer.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Should receive the event from the second connection.
	select {
	case evt := <-ch:
		if evt == nil {
			t.Fatal("received nil event")
		}
		if evt.Text != "reconnected" {
			t.Errorf("text = %q, want %q", evt.Text, "reconnected")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for reconnected event")
	}

	got := int(connectCount.Load())
	if got < 2 {
		t.Errorf("connect count = %d, want >= 2", got)
	}
}

// TestListen_ReconnectsOnConnectionError verifies reconnect when the
// WebSocket connection drops unexpectedly.
func TestListen_ReconnectsOnConnectionError(t *testing.T) {
	var connectCount atomic.Int32

	upgrader := websocket.Upgrader{}

	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		n := connectCount.Add(1)

		if n == 1 {
			// First connection: close immediately to simulate error.
			conn.Close()
			return
		}

		defer conn.Close()

		// Second connection: send event.
		env := socketEnvelope{
			EnvelopeID: "e3",
			Type:       EnvelopeTypeEventsAPI,
			Payload: mustMarshal(eventsAPIPayload{
				Type: "event_callback",
				Event: mustMarshal(innerEvent{
					Type:    EventTypeMessage,
					Channel: "C777",
					User:    "U222",
					Text:    "after error",
					TS:      "8888888888.888888",
				}),
			}),
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		data, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer apiServer.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-ch:
		if evt == nil {
			t.Fatal("received nil event")
		}
		if evt.Text != "after error" {
			t.Errorf("text = %q, want %q", evt.Text, "after error")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event after reconnect")
	}
}

// TestListen_ContextCancellationStopsReconnect verifies that cancelling
// the context stops the reconnect loop and closes the channel.
func TestListen_ContextCancellationStopsReconnect(t *testing.T) {
	upgrader := websocket.Upgrader{}

	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Keep reading until closed.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		data, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer apiServer.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiServer.URL)
	ctx, cancel := context.WithCancel(context.Background())

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Give time for connection to establish.
	time.Sleep(100 * time.Millisecond)

	// Cancel context.
	cancel()

	// Channel should close.
	select {
	case _, ok := <-ch:
		if ok {
			// Might get a stale event; drain and check again.
			for range ch {
			}
		}
		// Channel closed — success.
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for channel close after context cancel")
	}
}

// TestListen_BotMessagesFiltered verifies that bot messages are not emitted.
func TestListen_BotMessagesFiltered(t *testing.T) {
	upgrader := websocket.Upgrader{}

	wsMux := http.NewServeMux()
	wsMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send a bot message (should be filtered).
		botEnv := socketEnvelope{
			EnvelopeID: "bot-1",
			Type:       EnvelopeTypeEventsAPI,
			Payload: mustMarshal(eventsAPIPayload{
				Type: "event_callback",
				Event: mustMarshal(innerEvent{
					Type:    EventTypeMessage,
					Channel: "C123",
					User:    "U000",
					Text:    "bot says hi",
					TS:      "1111111111.111111",
					BotID:   "B999",
				}),
			}),
		}
		data, _ := json.Marshal(botEnv)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack

		// Send a human message (should pass through).
		humanEnv := socketEnvelope{
			EnvelopeID: "human-1",
			Type:       EnvelopeTypeEventsAPI,
			Payload: mustMarshal(eventsAPIPayload{
				Type: "event_callback",
				Event: mustMarshal(innerEvent{
					Type:    EventTypeMessage,
					Channel: "C123",
					User:    "U456",
					Text:    "human says hi",
					TS:      "2222222222.222222",
				}),
			}),
		}
		data, _ = json.Marshal(humanEnv)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	wsServer := httptest.NewServer(wsMux)
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http") + "/ws"

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := connectionsOpenResponse{OK: true, URL: wsURL}
		data, _ := json.Marshal(resp)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer apiServer.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiServer.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-ch:
		if evt == nil {
			t.Fatal("received nil event")
		}
		// Should be the human message, bot message was filtered.
		if evt.Text != "human says hi" {
			t.Errorf("text = %q, want %q", evt.Text, "human says hi")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for event")
	}
}

func TestNextBackoff(t *testing.T) {
	tests := []struct {
		current time.Duration
		want    time.Duration
	}{
		{1 * time.Second, 2 * time.Second},
		{2 * time.Second, 4 * time.Second},
		{4 * time.Second, 8 * time.Second},
		{8 * time.Second, 16 * time.Second},
		{16 * time.Second, 30 * time.Second}, // capped at max
		{30 * time.Second, 30 * time.Second}, // stays at max
	}

	for _, tt := range tests {
		got := nextBackoff(tt.current)
		if got != tt.want {
			t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.want)
		}
	}
}

// mustMarshal is a test helper that marshals v to json.RawMessage.
func mustMarshal(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

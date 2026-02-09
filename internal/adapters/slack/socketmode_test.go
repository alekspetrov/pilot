package slack

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
	"github.com/gorilla/websocket"
)

// testWSServer creates a WebSocket test server that accepts connections,
// sends the provided messages, then closes.
func testWSServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		handler(conn)
	}))
	return srv
}

// TestSocketClient_SuccessfulConnect verifies connect → hello → clean shutdown.
func TestSocketClient_SuccessfulConnect(t *testing.T) {
	helloDone := make(chan struct{})

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		// Send hello envelope.
		hello := envelope{Type: "hello"}
		if err := conn.WriteJSON(hello); err != nil {
			t.Logf("write hello: %v", err)
			return
		}
		close(helloDone)

		// Wait then close to trigger disconnect.
		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	// Mock the apps.connections.open endpoint.
	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+testutil.FakeSlackAppToken {
			t.Errorf("Authorization = %q, want Bearer %s", auth, testutil.FakeSlackAppToken)
		}
		resp := connectResponse{OK: true, URL: wsURL}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- sc.Run(ctx) }()

	select {
	case <-helloDone:
		// Hello was received — success.
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for hello")
	}

	cancel()
	<-errCh
}

// TestSocketClient_AuthFailure verifies that an auth error from apps.connections.open
// is surfaced correctly without crashing.
func TestSocketClient_AuthFailure(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := connectResponse{OK: false, Error: "invalid_auth"}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := sc.runOnce(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_auth") {
		t.Errorf("error = %q, want to contain 'invalid_auth'", err.Error())
	}
}

// TestSocketClient_MessageParsing verifies that incoming message events
// are dispatched to the onMessage callback with correct fields.
func TestSocketClient_MessageParsing(t *testing.T) {
	var (
		gotChannel string
		gotUser    string
		gotText    string
		received   = make(chan struct{}, 1)
	)

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		// Send a message event.
		env := map[string]interface{}{
			"envelope_id": "eid-1",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "message",
					"user":    "U123",
					"text":    "hello pilot",
					"channel": "C456",
				},
			},
		}
		if err := conn.WriteJSON(env); err != nil {
			t.Logf("write event: %v", err)
			return
		}

		// Read the acknowledge.
		_, _, _ = conn.ReadMessage()

		// Wait then close.
		time.Sleep(100 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(connectResponse{OK: true, URL: wsURL})
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
		WithOnMessage(func(channel, user, text string) {
			gotChannel = channel
			gotUser = user
			gotText = text
			select {
			case received <- struct{}{}:
			default:
			}
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go func() { _ = sc.Run(ctx) }()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	if gotChannel != "C456" {
		t.Errorf("channel = %q, want C456", gotChannel)
	}
	if gotUser != "U123" {
		t.Errorf("user = %q, want U123", gotUser)
	}
	if gotText != "hello pilot" {
		t.Errorf("text = %q, want 'hello pilot'", gotText)
	}
	cancel()
}

// TestSocketClient_AppMentionBotIDStripping verifies that <@BOT_ID> prefix
// is stripped from app_mention events.
func TestSocketClient_AppMentionBotIDStripping(t *testing.T) {
	var (
		gotText  string
		received = make(chan struct{}, 1)
	)

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		env := map[string]interface{}{
			"envelope_id": "eid-2",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "app_mention",
					"user":    "U999",
					"text":    "<@BBOT> deploy staging",
					"channel": "C789",
				},
			},
		}
		_ = conn.WriteJSON(env)
		_, _, _ = conn.ReadMessage() // ack
		time.Sleep(100 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(connectResponse{OK: true, URL: wsURL})
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
		WithBotID("BBOT"),
		WithOnMessage(func(_, _, text string) {
			gotText = text
			select {
			case received <- struct{}{}:
			default:
			}
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { _ = sc.Run(ctx) }()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for app_mention")
	}

	if gotText != "deploy staging" {
		t.Errorf("text = %q, want 'deploy staging'", gotText)
	}
	cancel()
}

// TestSocketClient_BotSelfMessageFiltering verifies that messages from the bot
// itself are not delivered to the callback.
func TestSocketClient_BotSelfMessageFiltering(t *testing.T) {
	callbackCalled := false
	var mu sync.Mutex

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		// Send a message FROM the bot (bot_id set).
		env := map[string]interface{}{
			"envelope_id": "eid-3",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "message",
					"user":    "BBOT",
					"text":    "I am the bot",
					"channel": "C111",
					"bot_id":  "BBOT_INTERNAL",
				},
			},
		}
		_ = conn.WriteJSON(env)
		_, _, _ = conn.ReadMessage() // ack

		// Also send an app_mention from the bot itself.
		env2 := map[string]interface{}{
			"envelope_id": "eid-4",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "app_mention",
					"user":    "BBOT",
					"text":    "<@BBOT> self-mention",
					"channel": "C111",
				},
			},
		}
		_ = conn.WriteJSON(env2)
		_, _, _ = conn.ReadMessage() // ack

		time.Sleep(100 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(connectResponse{OK: true, URL: wsURL})
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
		WithBotID("BBOT"),
		WithOnMessage(func(_, _, _ string) {
			mu.Lock()
			callbackCalled = true
			mu.Unlock()
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_ = sc.runOnce(ctx)

	mu.Lock()
	defer mu.Unlock()
	if callbackCalled {
		t.Error("onMessage called for bot self-message; should have been filtered")
	}
}

// TestSocketClient_DisconnectReconnect verifies that after a disconnect,
// the client reconnects automatically.
func TestSocketClient_DisconnectReconnect(t *testing.T) {
	var connectCount int
	var mu sync.Mutex
	reconnected := make(chan struct{}, 1)

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()

		// First connection: close immediately to trigger reconnect.
		if count == 1 {
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			return
		}

		// Second connection: signal reconnect, send hello, stay alive.
		select {
		case reconnected <- struct{}{}:
		default:
		}
		_ = conn.WriteJSON(envelope{Type: "hello"})
		time.Sleep(200 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(connectResponse{OK: true, URL: wsURL})
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
		withReconnectDelay(10*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() { _ = sc.Run(ctx) }()

	select {
	case <-reconnected:
		// Reconnect confirmed.
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for reconnect")
	}

	cancel()

	mu.Lock()
	defer mu.Unlock()
	if connectCount < 2 {
		t.Errorf("connectCount = %d, want >= 2 (reconnect happened)", connectCount)
	}
}

// TestSocketClient_AcknowledgeSent verifies that each envelope with an ID
// gets an acknowledgement sent back over the WebSocket.
func TestSocketClient_AcknowledgeSent(t *testing.T) {
	var (
		acks   []string
		ackMu  sync.Mutex
		allAck = make(chan struct{}, 1)
	)

	wsSrv := testWSServer(t, func(conn *websocket.Conn) {
		// Send 3 envelopes with IDs.
		for i, eid := range []string{"ack-1", "ack-2", "ack-3"} {
			env := map[string]interface{}{
				"envelope_id": eid,
				"type":        "events_api",
				"payload": map[string]interface{}{
					"event": map[string]interface{}{
						"type":    "message",
						"user":    "U123",
						"text":    "msg",
						"channel": "C456",
					},
				},
			}
			_ = conn.WriteJSON(env)

			// Read the ack.
			_, raw, err := conn.ReadMessage()
			if err != nil {
				t.Logf("read ack %d: %v", i, err)
				return
			}
			var ack struct {
				EnvelopeID string `json:"envelope_id"`
			}
			if err := json.Unmarshal(raw, &ack); err == nil {
				ackMu.Lock()
				acks = append(acks, ack.EnvelopeID)
				if len(acks) == 3 {
					select {
					case allAck <- struct{}{}:
					default:
					}
				}
				ackMu.Unlock()
			}
		}

		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	})
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(connectResponse{OK: true, URL: wsURL})
	}))
	defer apiSrv.Close()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(slog.Default()),
		withConnectURL(apiSrv.URL),
		withDialer(websocket.DefaultDialer),
		WithOnMessage(func(_, _, _ string) {}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { _ = sc.Run(ctx) }()

	select {
	case <-allAck:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for all acks")
	}

	ackMu.Lock()
	defer ackMu.Unlock()
	expected := []string{"ack-1", "ack-2", "ack-3"}
	if len(acks) != len(expected) {
		t.Fatalf("got %d acks, want %d", len(acks), len(expected))
	}
	for i, want := range expected {
		if acks[i] != want {
			t.Errorf("ack[%d] = %q, want %q", i, acks[i], want)
		}
	}
	cancel()
}

// TestStripBotMention tests the bot mention stripping logic.
func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name   string
		botID  string
		text   string
		expect string
	}{
		{
			name:   "with bot mention prefix",
			botID:  "B123",
			text:   "<@B123> deploy staging",
			expect: "deploy staging",
		},
		{
			name:   "no bot mention",
			botID:  "B123",
			text:   "deploy staging",
			expect: "deploy staging",
		},
		{
			name:   "empty bot ID",
			botID:  "",
			text:   "<@B123> deploy staging",
			expect: "<@B123> deploy staging",
		},
		{
			name:   "different bot ID in mention",
			botID:  "B999",
			text:   "<@B123> deploy staging",
			expect: "<@B123> deploy staging",
		},
		{
			name:   "mention only no trailing text",
			botID:  "B123",
			text:   "<@B123>",
			expect: "",
		},
		{
			name:   "mention with extra whitespace",
			botID:  "B123",
			text:   "<@B123>   lots of spaces  ",
			expect: "lots of spaces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := &SocketClient{botID: tt.botID}
			got := sc.stripBotMention(tt.text)
			if got != tt.expect {
				t.Errorf("stripBotMention(%q) = %q, want %q", tt.text, got, tt.expect)
			}
		})
	}
}

// TestNewSocketClient_Options tests that options are applied correctly.
func TestNewSocketClient_Options(t *testing.T) {
	called := false
	cb := func(_, _, _ string) { called = true }
	logger := slog.Default()

	sc := NewSocketClient(testutil.FakeSlackAppToken,
		WithLogger(logger),
		WithBotID("B42"),
		WithOnMessage(cb),
		WithPingInterval(10*time.Second),
	)

	if sc.appToken != testutil.FakeSlackAppToken {
		t.Errorf("appToken = %q, want %q", sc.appToken, testutil.FakeSlackAppToken)
	}
	if sc.botID != "B42" {
		t.Errorf("botID = %q, want B42", sc.botID)
	}
	if sc.pingInterval != 10*time.Second {
		t.Errorf("pingInterval = %v, want 10s", sc.pingInterval)
	}
	if sc.log != logger {
		t.Error("logger not set correctly")
	}
	if sc.onMessage == nil {
		t.Error("onMessage is nil")
	}
	// Invoke to verify it's the right callback.
	sc.onMessage("", "", "")
	if !called {
		t.Error("onMessage callback not wired correctly")
	}
}

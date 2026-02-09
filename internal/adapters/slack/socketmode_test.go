package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
	"github.com/gorilla/websocket"
)

// TestOpenConnection tests the apps.connections.open API call.
func TestOpenConnection(t *testing.T) {
	tests := []struct {
		name       string
		response   connectionsOpenResponse
		statusCode int
		wantURL    string
		wantErr    bool
		errContain string
	}{
		{
			name: "successful connection",
			response: connectionsOpenResponse{
				OK:  true,
				URL: "wss://wss-primary.slack.com/link/?ticket=abc123",
			},
			statusCode: http.StatusOK,
			wantURL:    "wss://wss-primary.slack.com/link/?ticket=abc123",
			wantErr:    false,
		},
		{
			name: "invalid auth",
			response: connectionsOpenResponse{
				OK:    false,
				Error: "invalid_auth",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "invalid_auth",
		},
		{
			name: "not allowed token type",
			response: connectionsOpenResponse{
				OK:    false,
				Error: "not_allowed_token_type",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "not_allowed_token_type",
		},
		{
			name: "empty URL in response",
			response: connectionsOpenResponse{
				OK:  true,
				URL: "",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "empty WSS URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify method
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}

				// Verify authorization header
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Authorization = %q, want Bearer prefix", auth)
				}
				token := strings.TrimPrefix(auth, "Bearer ")
				if token != testutil.FakeSlackAppToken {
					t.Errorf("token = %q, want %q", token, testutil.FakeSlackAppToken)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := NewSocketModeClient(testutil.FakeSlackAppToken)
			client.apiURL = server.URL

			ctx := context.Background()
			url, err := client.OpenConnection(ctx)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if url != tt.wantURL {
					t.Errorf("url = %q, want %q", url, tt.wantURL)
				}
			}
		})
	}
}

// TestOpenConnectionContextCancelled tests context cancellation.
func TestOpenConnectionContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(connectionsOpenResponse{OK: true, URL: "wss://test"})
	}))
	defer server.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken)
	client.apiURL = server.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.OpenConnection(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestOpenConnectionInvalidJSON tests handling of invalid JSON response.
func TestOpenConnectionInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken)
	client.apiURL = server.URL

	ctx := context.Background()
	_, err := client.OpenConnection(ctx)
	if err == nil {
		t.Fatal("expected error from invalid JSON")
	}
	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("error = %q, want to contain 'parse response'", err.Error())
	}
}

// TestParseEnvelope tests envelope parsing for all envelope types.
func TestParseEnvelope(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantType   string
		wantID     string
		wantErr    bool
		errContain string
	}{
		{
			name:     "hello envelope",
			input:    `{"envelope_id":"","type":"hello","payload":{}}`,
			wantType: "hello",
			wantID:   "",
			wantErr:  false,
		},
		{
			name:     "disconnect envelope",
			input:    `{"envelope_id":"","type":"disconnect","reason":"link_disabled","payload":{}}`,
			wantType: "disconnect",
			wantID:   "",
			wantErr:  false,
		},
		{
			name:     "events_api envelope",
			input:    `{"envelope_id":"abc-123","type":"events_api","payload":{"event":{"type":"message","channel":"C123","user":"U456","text":"hello"}}}`,
			wantType: "events_api",
			wantID:   "abc-123",
			wantErr:  false,
		},
		{
			name:       "invalid JSON",
			input:      `not json`,
			wantErr:    true,
			errContain: "unmarshal envelope",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := parseEnvelope([]byte(tt.input))

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if env.Type != tt.wantType {
				t.Errorf("type = %q, want %q", env.Type, tt.wantType)
			}
			if env.EnvelopeID != tt.wantID {
				t.Errorf("envelope_id = %q, want %q", env.EnvelopeID, tt.wantID)
			}
		})
	}
}

// TestParseSocketEvent tests event extraction from events_api payloads.
func TestParseSocketEvent(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantEvent *SocketEvent
		wantErr   bool
	}{
		{
			name:    "message event",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"hello world","ts":"1234567890.123456"}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "hello world",
				Timestamp: "1234567890.123456",
			},
		},
		{
			name:    "app_mention event strips bot mention",
			payload: `{"event":{"type":"app_mention","channel":"C789","user":"U111","text":"<@U222BOT> deploy to prod","ts":"1234567890.654321"}}`,
			wantEvent: &SocketEvent{
				Type:      "app_mention",
				ChannelID: "C789",
				UserID:    "U111",
				Text:      "deploy to prod",
				Timestamp: "1234567890.654321",
			},
		},
		{
			name:    "app_mention without bot mention prefix",
			payload: `{"event":{"type":"app_mention","channel":"C789","user":"U111","text":"deploy to prod","ts":"1234567890.654321"}}`,
			wantEvent: &SocketEvent{
				Type:      "app_mention",
				ChannelID: "C789",
				UserID:    "U111",
				Text:      "deploy to prod",
				Timestamp: "1234567890.654321",
			},
		},
		{
			name:    "threaded message",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"reply","thread_ts":"1234567890.000000","ts":"1234567890.123456"}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "reply",
				ThreadTS:  "1234567890.000000",
				Timestamp: "1234567890.123456",
			},
		},
		{
			name:    "message with files",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"see attached","ts":"1234567890.123456","files":[{"id":"F01","name":"doc.pdf","mimetype":"application/pdf","url_private":"https://files.slack.com/files/F01/doc.pdf"}]}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "see attached",
				Timestamp: "1234567890.123456",
				Files: []SlackFile{
					{
						ID:       "F01",
						Name:     "doc.pdf",
						MimeType: "application/pdf",
						URL:      "https://files.slack.com/files/F01/doc.pdf",
					},
				},
			},
		},
		{
			name:      "bot message ignored",
			payload:   `{"event":{"type":"message","channel":"C123","bot_id":"B999","text":"bot says hi","ts":"1234567890.123456"}}`,
			wantEvent: nil,
		},
		{
			name:      "bot_message subtype ignored",
			payload:   `{"event":{"type":"message","channel":"C123","user":"U456","subtype":"bot_message","text":"bot says hi","ts":"1234567890.123456"}}`,
			wantEvent: nil,
		},
		{
			name:      "unsupported event type ignored",
			payload:   `{"event":{"type":"reaction_added","channel":"C123","user":"U456"}}`,
			wantEvent: nil,
		},
		{
			name:    "invalid JSON",
			payload: `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, err := parseSocketEvent(json.RawMessage(tt.payload))

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantEvent == nil {
				if ev != nil {
					t.Errorf("expected nil event, got %+v", ev)
				}
				return
			}

			if ev == nil {
				t.Fatal("expected event, got nil")
			}

			if ev.Type != tt.wantEvent.Type {
				t.Errorf("Type = %q, want %q", ev.Type, tt.wantEvent.Type)
			}
			if ev.ChannelID != tt.wantEvent.ChannelID {
				t.Errorf("ChannelID = %q, want %q", ev.ChannelID, tt.wantEvent.ChannelID)
			}
			if ev.UserID != tt.wantEvent.UserID {
				t.Errorf("UserID = %q, want %q", ev.UserID, tt.wantEvent.UserID)
			}
			if ev.Text != tt.wantEvent.Text {
				t.Errorf("Text = %q, want %q", ev.Text, tt.wantEvent.Text)
			}
			if ev.ThreadTS != tt.wantEvent.ThreadTS {
				t.Errorf("ThreadTS = %q, want %q", ev.ThreadTS, tt.wantEvent.ThreadTS)
			}
			if ev.Timestamp != tt.wantEvent.Timestamp {
				t.Errorf("Timestamp = %q, want %q", ev.Timestamp, tt.wantEvent.Timestamp)
			}
			if len(ev.Files) != len(tt.wantEvent.Files) {
				t.Errorf("Files len = %d, want %d", len(ev.Files), len(tt.wantEvent.Files))
			} else {
				for i, f := range ev.Files {
					wf := tt.wantEvent.Files[i]
					if f.ID != wf.ID {
						t.Errorf("Files[%d].ID = %q, want %q", i, f.ID, wf.ID)
					}
					if f.Name != wf.Name {
						t.Errorf("Files[%d].Name = %q, want %q", i, f.Name, wf.Name)
					}
					if f.MimeType != wf.MimeType {
						t.Errorf("Files[%d].MimeType = %q, want %q", i, f.MimeType, wf.MimeType)
					}
					if f.URL != wf.URL {
						t.Errorf("Files[%d].URL = %q, want %q", i, f.URL, wf.URL)
					}
				}
			}
		})
	}
}

// TestBotMentionRegex tests the <@BOTID> stripping regex.
func TestBotMentionRegex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "standard mention",
			input: "<@U123BOT> deploy",
			want:  "deploy",
		},
		{
			name:  "mention with extra spaces",
			input: "<@U123BOT>   deploy to prod",
			want:  "deploy to prod",
		},
		{
			name:  "no mention",
			input: "deploy to prod",
			want:  "deploy to prod",
		},
		{
			name:  "mention only",
			input: "<@U123BOT>",
			want:  "",
		},
		{
			name:  "mention in middle not stripped",
			input: "hey <@U123BOT> deploy",
			want:  "hey <@U123BOT> deploy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := botMentionRegex.ReplaceAllString(tt.input, "")
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// TestAcknowledge tests the acknowledge frame format.
func TestAcknowledge(t *testing.T) {
	tests := []struct {
		name       string
		envelopeID string
	}{
		{
			name:       "standard envelope ID",
			envelopeID: "abc-123-def-456",
		},
		{
			name:       "UUID style envelope ID",
			envelopeID: "550e8400-e29b-41d4-a716-446655440000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a mock WebSocket server
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool { return true },
			}

			msgCh := make(chan []byte, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upgrade: %v", err)
					return
				}
				defer func() { _ = conn.Close() }()

				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				// Copy the message to avoid sharing the WebSocket read buffer.
				cp := make([]byte, len(msg))
				copy(cp, msg)
				msgCh <- cp
			}))
			defer server.Close()

			// Connect to the mock server
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer func() { _ = conn.Close() }()

			client := NewSocketModeClient(testutil.FakeSlackAppToken)
			err = client.acknowledge(conn, tt.envelopeID)
			if err != nil {
				t.Fatalf("acknowledge: %v", err)
			}

			// Wait for server to receive with timeout
			var receivedMsg []byte
			select {
			case receivedMsg = <-msgCh:
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for server to receive ack message")
			}

			// Parse and verify
			ack, err := parseAcknowledge(receivedMsg)
			if err != nil {
				t.Fatalf("parse ack: %v", err)
			}

			if ack.EnvelopeID != tt.envelopeID {
				t.Errorf("envelope_id = %q, want %q", ack.EnvelopeID, tt.envelopeID)
			}
		})
	}
}

// TestListenReceivesEvents tests the full Listen pipeline with a mock WebSocket server.
func TestListenReceivesEvents(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Mock WebSocket server that sends events
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send hello
		hello := `{"envelope_id":"","type":"hello","payload":{}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(hello))

		// Send a message event
		msgEvent := `{"envelope_id":"env-001","type":"events_api","payload":{"event":{"type":"message","channel":"C123","user":"U456","text":"test message","ts":"111.222"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(msgEvent))

		// Read ack for the message event
		_, ackData, err := conn.ReadMessage()
		if err != nil {
			return
		}
		ack, err := parseAcknowledge(ackData)
		if err != nil {
			t.Errorf("failed to parse ack: %v", err)
			return
		}
		if ack.EnvelopeID != "env-001" {
			t.Errorf("ack envelope_id = %q, want %q", ack.EnvelopeID, "env-001")
		}

		// Send an app_mention event
		mentionEvent := `{"envelope_id":"env-002","type":"events_api","payload":{"event":{"type":"app_mention","channel":"C789","user":"U111","text":"<@U222BOT> do the thing","ts":"333.444"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(mentionEvent))

		// Read ack
		_, _, _ = conn.ReadMessage()

		// Keep connection open until context cancelled
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	// Mock API server for apps.connections.open
	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connectionsOpenResponse{
			OK:  true,
			URL: wsURL,
		})
	}))
	defer apiServer.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken)
	client.apiURL = apiServer.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Collect events
	var received []*SocketEvent
	timeout := time.After(3 * time.Second)

	for len(received) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("events channel closed unexpectedly")
			}
			received = append(received, ev)
		case <-timeout:
			t.Fatalf("timed out waiting for events, got %d", len(received))
		}
	}

	cancel() // Clean shutdown

	// Verify first event (message)
	if received[0].Type != "message" {
		t.Errorf("event[0].Type = %q, want %q", received[0].Type, "message")
	}
	if received[0].ChannelID != "C123" {
		t.Errorf("event[0].ChannelID = %q, want %q", received[0].ChannelID, "C123")
	}
	if received[0].Text != "test message" {
		t.Errorf("event[0].Text = %q, want %q", received[0].Text, "test message")
	}

	// Verify second event (app_mention with stripped prefix)
	if received[1].Type != "app_mention" {
		t.Errorf("event[1].Type = %q, want %q", received[1].Type, "app_mention")
	}
	if received[1].ChannelID != "C789" {
		t.Errorf("event[1].ChannelID = %q, want %q", received[1].ChannelID, "C789")
	}
	if received[1].Text != "do the thing" {
		t.Errorf("event[1].Text = %q, want %q", received[1].Text, "do the thing")
	}
}

// TestListenFiltersBotMessages tests that bot messages are filtered out.
func TestListenFiltersBotMessages(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send hello
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))

		// Send bot message (should be filtered)
		botMsg := `{"envelope_id":"env-bot","type":"events_api","payload":{"event":{"type":"message","channel":"C123","bot_id":"B999","text":"bot says hi","ts":"111.222"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(botMsg))

		// Read ack for bot message
		_, _, _ = conn.ReadMessage()

		// Send real user message
		userMsg := `{"envelope_id":"env-user","type":"events_api","payload":{"event":{"type":"message","channel":"C123","user":"U456","text":"user says hi","ts":"333.444"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(userMsg))

		// Read ack
		_, _, _ = conn.ReadMessage()

		// Keep alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connectionsOpenResponse{OK: true, URL: wsURL})
	}))
	defer apiServer.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken)
	client.apiURL = apiServer.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Should only get the user message (bot message filtered)
	select {
	case ev := <-events:
		if ev.Text != "user says hi" {
			t.Errorf("Text = %q, want %q", ev.Text, "user says hi")
		}
		if ev.UserID != "U456" {
			t.Errorf("UserID = %q, want %q", ev.UserID, "U456")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for user event")
	}

	cancel()
}

// TestListenHandlesDisconnect tests that disconnect envelopes trigger reconnection.
func TestListenHandlesDisconnect(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	connectionCount := 0

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		connectionCount++

		// Send hello
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"hello"}`))

		if connectionCount == 1 {
			// First connection: send disconnect
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"disconnect","reason":"link_disabled"}`))
			return
		}

		// Second connection: send a message and keep alive
		msg := `{"envelope_id":"env-reconnect","type":"events_api","payload":{"event":{"type":"message","channel":"C123","user":"U456","text":"after reconnect","ts":"555.666"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(msg))

		// Read ack
		_, _, _ = conn.ReadMessage()

		// Keep alive
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(connectionsOpenResponse{OK: true, URL: wsURL})
	}))
	defer apiServer.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken)
	client.apiURL = apiServer.URL

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Should receive the message from the second connection (after reconnect)
	select {
	case ev := <-events:
		if ev.Text != "after reconnect" {
			t.Errorf("Text = %q, want %q", ev.Text, "after reconnect")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for event after reconnect")
	}

	cancel()

	if connectionCount < 2 {
		t.Errorf("connectionCount = %d, want >= 2", connectionCount)
	}
}

// TestNewSocketModeClient tests client creation.
func TestNewSocketModeClient(t *testing.T) {
	client := NewSocketModeClient(testutil.FakeSlackAppToken)

	if client == nil {
		t.Fatal("NewSocketModeClient returned nil")
	}
	if client.appToken != testutil.FakeSlackAppToken {
		t.Errorf("appToken = %q, want %q", client.appToken, testutil.FakeSlackAppToken)
	}
	if client.httpClient == nil {
		t.Error("httpClient is nil")
	}
	if client.dialer == nil {
		t.Error("dialer is nil")
	}
	if client.log == nil {
		t.Error("log is nil")
	}
	if client.apiURL != connectionsOpenURL {
		t.Errorf("apiURL = %q, want %q", client.apiURL, connectionsOpenURL)
	}
}

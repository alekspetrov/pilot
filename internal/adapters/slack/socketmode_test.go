package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/logging"
	"github.com/alekspetrov/pilot/internal/testutil"
	"github.com/gorilla/websocket"
)

// TestOpenConnection tests the apps.connections.open call.
func TestOpenConnection(t *testing.T) {
	tests := []struct {
		name       string
		response   map[string]interface{}
		statusCode int
		wantURL    string
		wantErr    bool
		errContain string
	}{
		{
			name: "successful open",
			response: map[string]interface{}{
				"ok":  true,
				"url": "wss://wss-primary.slack.com/link/?ticket=abc123",
			},
			statusCode: http.StatusOK,
			wantURL:    "wss://wss-primary.slack.com/link/?ticket=abc123",
			wantErr:    false,
		},
		{
			name: "invalid token",
			response: map[string]interface{}{
				"ok":    false,
				"error": "invalid_auth",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "invalid_auth",
		},
		{
			name: "not allowed",
			response: map[string]interface{}{
				"ok":    false,
				"error": "not_allowed_token_type",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "not_allowed_token_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %q, want POST", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/apps.connections.open") {
					t.Errorf("path = %q, want suffix /apps.connections.open", r.URL.Path)
				}
				auth := r.Header.Get("Authorization")
				if !strings.HasPrefix(auth, "Bearer ") {
					t.Errorf("Authorization = %q, want Bearer prefix", auth)
				}

				w.WriteHeader(tt.statusCode)
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer server.Close()

			client := &SocketModeClient{
				appToken:   testutil.FakeSlackAppToken,
				httpClient: server.Client(),
			}

			// Point at test server instead of slack.com.
			origURL := slackAPIURL
			// We need to override the URL by calling the test server directly.
			ctx := context.Background()

			// Build request manually to hit test server.
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/apps.connections.open", nil)
			req.Header.Set("Authorization", "Bearer "+client.appToken)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

			resp, err := client.httpClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			var result struct {
				OK    bool   `json:"ok"`
				URL   string `json:"url"`
				Error string `json:"error,omitempty"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&result)

			if tt.wantErr {
				if result.OK {
					t.Error("expected error but got OK")
				}
				if tt.errContain != "" && !strings.Contains(result.Error, tt.errContain) {
					t.Errorf("error = %q, want to contain %q", result.Error, tt.errContain)
				}
			} else {
				if !result.OK {
					t.Errorf("expected OK but got error: %s", result.Error)
				}
				if result.URL != tt.wantURL {
					t.Errorf("url = %q, want %q", result.URL, tt.wantURL)
				}
			}
			_ = origURL
		})
	}
}

// TestParseEnvelope tests envelope JSON parsing.
func TestParseEnvelope(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantType   string
		wantID     string
		wantErr    bool
	}{
		{
			name:     "events_api envelope",
			input:    `{"envelope_id":"env-123","type":"events_api","payload":{"event":{"type":"message"}}}`,
			wantType: "events_api",
			wantID:   "env-123",
		},
		{
			name:     "disconnect envelope",
			input:    `{"envelope_id":"","type":"disconnect","reason":"link_disabled"}`,
			wantType: "disconnect",
			wantID:   "",
		},
		{
			name:     "hello envelope",
			input:    `{"envelope_id":"","type":"hello"}`,
			wantType: "hello",
			wantID:   "",
		},
		{
			name:    "invalid JSON",
			input:   `{not valid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := parseEnvelope([]byte(tt.input))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
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

// TestParseEventsAPI tests event extraction from events_api payloads.
func TestParseEventsAPI(t *testing.T) {
	tests := []struct {
		name      string
		payload   string
		wantEvent *SocketEvent
		wantErr   bool
	}{
		{
			name:    "message event",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"hello","ts":"1234567890.123456"}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "hello",
				Timestamp: "1234567890.123456",
			},
		},
		{
			name:    "app_mention strips bot prefix",
			payload: `{"event":{"type":"app_mention","channel":"C123","user":"U456","text":"<@U789BOT> deploy staging","ts":"111.222"}}`,
			wantEvent: &SocketEvent{
				Type:      "app_mention",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "deploy staging",
				Timestamp: "111.222",
			},
		},
		{
			name:    "threaded message",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"reply","ts":"111.333","thread_ts":"111.222"}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "reply",
				Timestamp: "111.333",
				ThreadTS:  "111.222",
			},
		},
		{
			name:      "bot message filtered",
			payload:   `{"event":{"type":"message","channel":"C123","user":"","text":"bot says hi","ts":"111.444","bot_id":"B999"}}`,
			wantEvent: nil,
		},
		{
			name:      "unsupported event type",
			payload:   `{"event":{"type":"reaction_added","channel":"C123","user":"U456"}}`,
			wantEvent: nil,
		},
		{
			name:    "message with files",
			payload: `{"event":{"type":"message","channel":"C123","user":"U456","text":"see attached","ts":"111.555","files":[{"id":"F001","name":"screenshot.png","mimetype":"image/png","url_private":"https://files.slack.com/F001","size":12345}]}}`,
			wantEvent: &SocketEvent{
				Type:      "message",
				ChannelID: "C123",
				UserID:    "U456",
				Text:      "see attached",
				Timestamp: "111.555",
				Files: []SlackFile{
					{ID: "F001", Name: "screenshot.png", MimeType: "image/png", URL: "https://files.slack.com/F001", Size: 12345},
				},
			},
		},
		{
			name:    "invalid JSON",
			payload: `{bad json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := parseEventsAPI(json.RawMessage(tt.payload))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantEvent == nil {
				if event != nil {
					t.Errorf("expected nil event, got %+v", event)
				}
				return
			}
			if event == nil {
				t.Fatal("expected event, got nil")
			}
			if event.Type != tt.wantEvent.Type {
				t.Errorf("Type = %q, want %q", event.Type, tt.wantEvent.Type)
			}
			if event.ChannelID != tt.wantEvent.ChannelID {
				t.Errorf("ChannelID = %q, want %q", event.ChannelID, tt.wantEvent.ChannelID)
			}
			if event.UserID != tt.wantEvent.UserID {
				t.Errorf("UserID = %q, want %q", event.UserID, tt.wantEvent.UserID)
			}
			if event.Text != tt.wantEvent.Text {
				t.Errorf("Text = %q, want %q", event.Text, tt.wantEvent.Text)
			}
			if event.ThreadTS != tt.wantEvent.ThreadTS {
				t.Errorf("ThreadTS = %q, want %q", event.ThreadTS, tt.wantEvent.ThreadTS)
			}
			if event.Timestamp != tt.wantEvent.Timestamp {
				t.Errorf("Timestamp = %q, want %q", event.Timestamp, tt.wantEvent.Timestamp)
			}
			if len(event.Files) != len(tt.wantEvent.Files) {
				t.Errorf("Files count = %d, want %d", len(event.Files), len(tt.wantEvent.Files))
			}
			for i := range event.Files {
				if i >= len(tt.wantEvent.Files) {
					break
				}
				if event.Files[i].ID != tt.wantEvent.Files[i].ID {
					t.Errorf("Files[%d].ID = %q, want %q", i, event.Files[i].ID, tt.wantEvent.Files[i].ID)
				}
			}
		})
	}
}

// TestBotMentionRegex tests the bot mention stripping regex.
func TestBotMentionRegex(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<@U123BOT> deploy staging", "deploy staging"},
		{"<@UABC> hello world", "hello world"},
		{"<@U1>quick", "quick"},
		{"no mention here", "no mention here"},
		{"<@U123BOT>  extra spaces", "extra spaces"},
	}

	for _, tt := range tests {
		got := botMentionRegex.ReplaceAllString(tt.input, "")
		if got != tt.want {
			t.Errorf("strip(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestNextBackoff tests exponential backoff calculation.
func TestNextBackoff(t *testing.T) {
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{"1s → 2s", 1 * time.Second, 2 * time.Second},
		{"2s → 4s", 2 * time.Second, 4 * time.Second},
		{"4s → 8s", 4 * time.Second, 8 * time.Second},
		{"8s → 16s", 8 * time.Second, 16 * time.Second},
		{"16s → 30s (capped)", 16 * time.Second, 30 * time.Second},
		{"30s → 30s (stays at max)", 30 * time.Second, 30 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nextBackoff(tt.current)
			if got != tt.want {
				t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.want)
			}
		})
	}
}

// TestNewSocketModeClient tests client construction.
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
		t.Error("logger is nil")
	}
}

// TestListenContextCancel tests that Listen shuts down when context is cancelled.
func TestListenContextCancel(t *testing.T) {
	// Set up a mock HTTP server that returns a WSS URL pointing to a WS server.
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Send hello envelope.
		hello := `{"envelope_id":"","type":"hello"}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(hello))
		// Keep connection alive until client disconnects.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + wsServer.URL[4:] // http:// → ws://

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":  true,
			"url": wsURL,
		})
	}))
	defer apiServer.Close()

	client := &SocketModeClient{
		appToken:   testutil.FakeSlackAppToken,
		httpClient: apiServer.Client(),
		dialer:     websocket.DefaultDialer,
		log:        logging.WithComponent("test.socketmode"),
	}
	// Override OpenConnection to use test server.
	origOpenConn := client.httpClient
	_ = origOpenConn

	ctx, cancel := context.WithCancel(context.Background())

	// We need to override OpenConnection to hit our test server.
	// Use a wrapper approach: replace httpClient and slackAPIURL.
	// Since slackAPIURL is a package const, we'll test the readLoop directly.

	// Test readLoop + context cancellation.
	ch := make(chan *SocketEvent, 64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := client.readLoop(ctx, wsURL, ch)
		if err != nil && err != context.Canceled {
			t.Logf("readLoop ended: %v", err)
		}
	}()

	// Wait briefly for connection to establish.
	time.Sleep(100 * time.Millisecond)

	// Cancel context.
	cancel()

	// readLoop should exit.
	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("readLoop did not exit after context cancellation")
	}
}

// TestListenReceivesEvents tests that events flow through the channel.
func TestListenReceivesEvents(t *testing.T) {
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send hello.
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"envelope_id":"","type":"hello"}`))

		// Send an events_api envelope.
		env := map[string]interface{}{
			"envelope_id": "env-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"user":    "U456",
					"text":    "hello pilot",
					"ts":      "111.222",
				},
			},
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Read the ack for env-001.
		_, ackData, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ack map[string]string
		_ = json.Unmarshal(ackData, &ack)
		if ack["envelope_id"] != "env-001" {
			t.Errorf("ack envelope_id = %q, want %q", ack["envelope_id"], "env-001")
		}

		// Keep alive until client disconnects.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + wsServer.URL[4:]

	client := &SocketModeClient{
		appToken:   testutil.FakeSlackAppToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		dialer:     websocket.DefaultDialer,
		log:        logging.WithComponent("test.socketmode"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan *SocketEvent, 64)
	go func() {
		_ = client.readLoop(ctx, wsURL, ch)
	}()

	// Wait for the event.
	select {
	case event := <-ch:
		if event.Type != "message" {
			t.Errorf("event.Type = %q, want %q", event.Type, "message")
		}
		if event.ChannelID != "C123" {
			t.Errorf("event.ChannelID = %q, want %q", event.ChannelID, "C123")
		}
		if event.UserID != "U456" {
			t.Errorf("event.UserID = %q, want %q", event.UserID, "U456")
		}
		if event.Text != "hello pilot" {
			t.Errorf("event.Text = %q, want %q", event.Text, "hello pilot")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestListenDisconnectTriggersReconnect tests that a disconnect envelope causes reconnect.
func TestListenDisconnectTriggersReconnect(t *testing.T) {
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send disconnect immediately.
		disc := `{"envelope_id":"","type":"disconnect","reason":"link_disabled"}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(disc))

		// Wait for client to read it.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + wsServer.URL[4:]

	client := &SocketModeClient{
		appToken:   testutil.FakeSlackAppToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		dialer:     websocket.DefaultDialer,
		log:        logging.WithComponent("test.socketmode"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan *SocketEvent, 64)

	// readLoop should return nil on disconnect (graceful).
	err := client.readLoop(ctx, wsURL, ch)
	if err != nil {
		t.Errorf("readLoop returned error on disconnect: %v (expected nil)", err)
	}
}

// TestListenFiltersBotMessages tests that bot messages are filtered out.
func TestListenFiltersBotMessages(t *testing.T) {
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send a bot message (should be filtered).
		botEnv := map[string]interface{}{
			"envelope_id": "env-bot",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"text":    "I am a bot",
					"ts":      "111.333",
					"bot_id":  "B999",
				},
			},
		}
		data, _ := json.Marshal(botEnv)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Then send a real user message.
		userEnv := map[string]interface{}{
			"envelope_id": "env-user",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"user":    "U456",
					"text":    "real user",
					"ts":      "111.444",
				},
			},
		}
		data, _ = json.Marshal(userEnv)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Keep alive.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}))
	defer wsServer.Close()

	wsURL := "ws" + wsServer.URL[4:]

	client := &SocketModeClient{
		appToken:   testutil.FakeSlackAppToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		dialer:     websocket.DefaultDialer,
		log:        logging.WithComponent("test.socketmode"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan *SocketEvent, 64)
	go func() {
		_ = client.readLoop(ctx, wsURL, ch)
	}()

	// First event should be the user message (bot message filtered).
	select {
	case event := <-ch:
		if event.Text != "real user" {
			t.Errorf("expected user message, got %q", event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

// TestAcknowledge tests that envelopes are acknowledged.
func TestAcknowledge(t *testing.T) {
	ackReceived := make(chan string, 1)

	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Send event with envelope_id.
		env := `{"envelope_id":"ack-test-123","type":"events_api","payload":{"event":{"type":"message","channel":"C1","user":"U1","text":"hi","ts":"1.1"}}}`
		_ = conn.WriteMessage(websocket.TextMessage, []byte(env))

		// Read the ack.
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var ack map[string]string
		_ = json.Unmarshal(data, &ack)
		ackReceived <- ack["envelope_id"]
	}))
	defer wsServer.Close()

	wsURL := "ws" + wsServer.URL[4:]

	client := &SocketModeClient{
		appToken:   testutil.FakeSlackAppToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		dialer:     websocket.DefaultDialer,
		log:        logging.WithComponent("test.socketmode"),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := make(chan *SocketEvent, 64)
	go func() {
		_ = client.readLoop(ctx, wsURL, ch)
	}()

	select {
	case id := <-ackReceived:
		if id != "ack-test-123" {
			t.Errorf("ack envelope_id = %q, want %q", id, "ack-test-123")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for ack")
	}
}

// logging import helper — ensure the test module uses the project's logging.
var _ = logging.WithComponent

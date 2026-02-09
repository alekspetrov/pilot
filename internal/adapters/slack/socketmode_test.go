package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
	"github.com/gorilla/websocket"
)

// --- helpers ---

// mockWSServer creates a WebSocket server that sends the given envelopes then closes.
// It collects all messages received from the client (acknowledgements).
func mockWSServer(t *testing.T, envelopes [][]byte) (*httptest.Server, *wsRecorder) {
	t.Helper()
	rec := &wsRecorder{}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		// Start reading acks in background
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					return
				}
				rec.add(msg)
			}
		}()

		// Send envelopes
		for _, env := range envelopes {
			if err := conn.WriteMessage(websocket.TextMessage, env); err != nil {
				return
			}
			// Small delay to allow client to process
			time.Sleep(10 * time.Millisecond)
		}

		// Give client time to ack before closing
		time.Sleep(50 * time.Millisecond)
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		<-done
	}))

	return srv, rec
}

// wsRecorder records messages received on the server side.
type wsRecorder struct {
	mu   sync.Mutex
	msgs [][]byte
}

func (r *wsRecorder) add(msg []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, msg)
}

func (r *wsRecorder) messages() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([][]byte, len(r.msgs))
	copy(cp, r.msgs)
	return cp
}

// mockConnectionsOpen returns an httptest.Server that responds to apps.connections.open
// with the given WebSocket URL.
func mockConnectionsOpen(t *testing.T, wssURL string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/apps.connections.open") {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth == "" {
			_ = json.NewEncoder(w).Encode(connectionsOpenResponse{OK: false, Error: "not_authed"})
			return
		}

		// Convert http URL to ws URL
		wsURL := strings.Replace(wssURL, "http://", "ws://", 1)
		_ = json.NewEncoder(w).Encode(connectionsOpenResponse{OK: true, URL: wsURL})
	}))
}

// makeEnvelope builds a Socket Mode envelope JSON.
func makeEnvelope(envelopeID, envType string, payload interface{}) []byte {
	data, _ := json.Marshal(payload)
	env := map[string]interface{}{
		"envelope_id": envelopeID,
		"type":        envType,
	}
	if payload != nil {
		env["payload"] = json.RawMessage(data)
	}
	b, _ := json.Marshal(env)
	return b
}

// makeMessageEnvelope creates a message event envelope.
func makeMessageEnvelope(envelopeID, channel, user, text, ts string) []byte {
	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "message",
			"channel": channel,
			"user":    user,
			"text":    text,
			"ts":      ts,
		},
	}
	return makeEnvelope(envelopeID, "events_api", payload)
}

// makeAppMentionEnvelope creates an app_mention event envelope.
func makeAppMentionEnvelope(envelopeID, channel, user, text, ts string) []byte {
	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "app_mention",
			"channel": channel,
			"user":    user,
			"text":    text,
			"ts":      ts,
		},
	}
	return makeEnvelope(envelopeID, "events_api", payload)
}

// makeBotMessageEnvelope creates a bot_message event envelope.
func makeBotMessageEnvelope(envelopeID, channel, botID, text, ts string) []byte {
	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "message",
			"channel": channel,
			"bot_id":  botID,
			"text":    text,
			"ts":      ts,
		},
	}
	return makeEnvelope(envelopeID, "events_api", payload)
}

// makeDisconnectEnvelope creates a disconnect envelope.
func makeDisconnectEnvelope(envelopeID, reason string) []byte {
	env := map[string]interface{}{
		"envelope_id": envelopeID,
		"type":        "disconnect",
		"reason":      reason,
	}
	b, _ := json.Marshal(env)
	return b
}

// makeHelloEnvelope creates a hello envelope.
func makeHelloEnvelope() []byte {
	env := map[string]interface{}{
		"envelope_id": "",
		"type":        "hello",
	}
	b, _ := json.Marshal(env)
	return b
}

// --- parseEnvelope tests ---

func TestParseEnvelope(t *testing.T) {
	tests := []struct {
		name         string
		input        []byte
		wantID       string
		wantType     string
		wantEvent    bool
		wantEventTyp string
		wantChannel  string
		wantUser     string
		wantText     string
		wantErr      bool
	}{
		{
			name:         "message event",
			input:        makeMessageEnvelope("env-1", "C123", "U456", "hello world", "1234.5678"),
			wantID:       "env-1",
			wantType:     "events_api",
			wantEvent:    true,
			wantEventTyp: "message",
			wantChannel:  "C123",
			wantUser:     "U456",
			wantText:     "hello world",
		},
		{
			name:         "app_mention strips mention",
			input:        makeAppMentionEnvelope("env-2", "C789", "U111", "<@UBOT123> deploy to prod", "1234.9999"),
			wantID:       "env-2",
			wantType:     "events_api",
			wantEvent:    true,
			wantEventTyp: "app_mention",
			wantChannel:  "C789",
			wantUser:     "U111",
			wantText:     "deploy to prod",
		},
		{
			name:      "bot message is filtered",
			input:     makeBotMessageEnvelope("env-3", "C123", "BBOT456", "I am a bot", "1234.0001"),
			wantID:    "env-3",
			wantType:  "events_api",
			wantEvent: false,
		},
		{
			name:     "disconnect envelope",
			input:    makeDisconnectEnvelope("env-4", "link_disabled"),
			wantID:   "env-4",
			wantType: "disconnect",
		},
		{
			name:     "hello envelope",
			input:    makeHelloEnvelope(),
			wantID:   "",
			wantType: "hello",
		},
		{
			name:    "invalid JSON",
			input:   []byte(`{broken`),
			wantErr: true,
		},
		{
			name:         "app_mention with multiple mentions",
			input:        makeAppMentionEnvelope("env-5", "C100", "U200", "<@UBOT1> <@UBOT2> check status", "1234.2222"),
			wantID:       "env-5",
			wantType:     "events_api",
			wantEvent:    true,
			wantEventTyp: "app_mention",
			wantText:     "check status",
		},
		{
			name: "message with thread_ts",
			input: func() []byte {
				payload := map[string]interface{}{
					"type": "event_callback",
					"event": map[string]interface{}{
						"type":      "message",
						"channel":   "C123",
						"user":      "U456",
						"text":      "thread reply",
						"ts":        "1234.6000",
						"thread_ts": "1234.5000",
					},
				}
				return makeEnvelope("env-6", "events_api", payload)
			}(),
			wantID:       "env-6",
			wantType:     "events_api",
			wantEvent:    true,
			wantEventTyp: "message",
			wantText:     "thread reply",
		},
		{
			name: "message subtype filtered",
			input: func() []byte {
				payload := map[string]interface{}{
					"type": "event_callback",
					"event": map[string]interface{}{
						"type":    "message",
						"subtype": "message_changed",
						"channel": "C123",
						"user":    "U456",
						"text":    "edited message",
						"ts":      "1234.7000",
					},
				}
				return makeEnvelope("env-7", "events_api", payload)
			}(),
			wantID:    "env-7",
			wantType:  "events_api",
			wantEvent: false,
		},
		{
			name: "unknown event type ignored",
			input: func() []byte {
				payload := map[string]interface{}{
					"type": "event_callback",
					"event": map[string]interface{}{
						"type":    "reaction_added",
						"channel": "C123",
						"user":    "U456",
					},
				}
				return makeEnvelope("env-8", "events_api", payload)
			}(),
			wantID:    "env-8",
			wantType:  "events_api",
			wantEvent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envID, envType, event, err := parseEnvelope(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if envID != tt.wantID {
				t.Errorf("envelope_id = %q, want %q", envID, tt.wantID)
			}
			if envType != tt.wantType {
				t.Errorf("type = %q, want %q", envType, tt.wantType)
			}

			if tt.wantEvent {
				if event == nil {
					t.Fatal("expected event, got nil")
				}
				if tt.wantEventTyp != "" && event.Type != tt.wantEventTyp {
					t.Errorf("event.Type = %q, want %q", event.Type, tt.wantEventTyp)
				}
				if tt.wantChannel != "" && event.ChannelID != tt.wantChannel {
					t.Errorf("event.ChannelID = %q, want %q", event.ChannelID, tt.wantChannel)
				}
				if tt.wantUser != "" && event.UserID != tt.wantUser {
					t.Errorf("event.UserID = %q, want %q", event.UserID, tt.wantUser)
				}
				if tt.wantText != "" && event.Text != tt.wantText {
					t.Errorf("event.Text = %q, want %q", event.Text, tt.wantText)
				}
			} else if event != nil {
				t.Errorf("expected nil event, got %+v", event)
			}
		})
	}
}

// --- stripMentions tests ---

func TestStripMentions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"single mention at start", "<@UBOT123> hello", "hello"},
		{"mention in middle", "hey <@UBOT123> deploy", "hey  deploy"},
		{"multiple mentions", "<@UBOT1> <@UBOT2> run tests", "run tests"},
		{"no mentions", "just text", "just text"},
		{"only mention", "<@UBOT123>", ""},
		{"mention with newline", "<@UBOT123>\ndo something", "do something"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMentions(tt.input)
			if got != tt.want {
				t.Errorf("stripMentions(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// --- OpenConnection tests ---

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
			name:       "successful connection",
			response:   connectionsOpenResponse{OK: true, URL: "wss://wss-primary.slack.com/link/?ticket=abc"},
			statusCode: http.StatusOK,
			wantURL:    "wss://wss-primary.slack.com/link/?ticket=abc",
		},
		{
			name:       "API error",
			response:   connectionsOpenResponse{OK: false, Error: "invalid_auth"},
			statusCode: http.StatusOK,
			wantErr:    true,
			errContain: "invalid_auth",
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify auth header uses app token
				auth := r.Header.Get("Authorization")
				if auth != "Bearer "+testutil.FakeSlackAppToken {
					t.Errorf("auth header = %q, want Bearer %s", auth, testutil.FakeSlackAppToken)
				}

				if tt.statusCode == http.StatusInternalServerError {
					w.WriteHeader(tt.statusCode)
					_, _ = w.Write([]byte("internal error"))
					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tt.response)
			}))
			defer srv.Close()

			client := NewSocketModeClient(testutil.FakeSlackAppToken, WithAPIURL(srv.URL))
			url, err := client.OpenConnection(context.Background())

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContain)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if url != tt.wantURL {
				t.Errorf("URL = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestOpenConnectionContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until context cancelled
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := NewSocketModeClient(testutil.FakeSlackAppToken, WithAPIURL(srv.URL))
	_, err := client.OpenConnection(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// --- Full integration: Listen with mock WebSocket ---

func TestListenMessageEvents(t *testing.T) {
	envelopes := [][]byte{
		makeHelloEnvelope(),
		makeMessageEnvelope("msg-1", "C100", "U200", "hello from user", "1111.0001"),
		makeMessageEnvelope("msg-2", "C100", "U300", "second message", "1111.0002"),
	}

	wsSrv, rec := mockWSServer(t, envelopes)
	defer wsSrv.Close()

	apiSrv := mockConnectionsOpen(t, wsSrv.URL)
	defer apiSrv.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithAPIURL(apiSrv.URL),
		WithReconnectBackoff(10*time.Millisecond, 50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	var events []*SocketEvent
	for evt := range ch {
		events = append(events, evt)
		if len(events) >= 2 {
			cancel()
		}
	}

	if len(events) < 2 {
		t.Fatalf("got %d events, want at least 2", len(events))
	}

	// Verify first event
	if events[0].Type != "message" {
		t.Errorf("events[0].Type = %q, want \"message\"", events[0].Type)
	}
	if events[0].Text != "hello from user" {
		t.Errorf("events[0].Text = %q, want \"hello from user\"", events[0].Text)
	}
	if events[0].ChannelID != "C100" {
		t.Errorf("events[0].ChannelID = %q, want \"C100\"", events[0].ChannelID)
	}
	if events[0].UserID != "U200" {
		t.Errorf("events[0].UserID = %q, want \"U200\"", events[0].UserID)
	}

	// Verify second event
	if events[1].Text != "second message" {
		t.Errorf("events[1].Text = %q, want \"second message\"", events[1].Text)
	}

	// Verify acks were sent (hello has empty envelope_id, msg-1, msg-2 should be acked)
	acks := rec.messages()
	ackIDs := make(map[string]bool)
	for _, msg := range acks {
		var ack struct {
			EnvelopeID string `json:"envelope_id"`
		}
		if json.Unmarshal(msg, &ack) == nil && ack.EnvelopeID != "" {
			ackIDs[ack.EnvelopeID] = true
		}
	}
	for _, wantID := range []string{"msg-1", "msg-2"} {
		if !ackIDs[wantID] {
			t.Errorf("envelope %q was not acknowledged", wantID)
		}
	}
}

func TestListenAppMentionStripsMentions(t *testing.T) {
	envelopes := [][]byte{
		makeAppMentionEnvelope("mention-1", "C100", "U200", "<@UBOTABC> deploy to staging", "2222.0001"),
	}

	wsSrv, rec := mockWSServer(t, envelopes)
	defer wsSrv.Close()
	_ = rec

	apiSrv := mockConnectionsOpen(t, wsSrv.URL)
	defer apiSrv.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithAPIURL(apiSrv.URL),
		WithReconnectBackoff(10*time.Millisecond, 50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	var event *SocketEvent
	for evt := range ch {
		event = evt
		cancel()
	}

	if event == nil {
		t.Fatal("expected app_mention event, got nil")
	}
	if event.Type != "app_mention" {
		t.Errorf("Type = %q, want \"app_mention\"", event.Type)
	}
	if event.Text != "deploy to staging" {
		t.Errorf("Text = %q, want \"deploy to staging\" (mention should be stripped)", event.Text)
	}
}

func TestListenFiltersBotMessages(t *testing.T) {
	envelopes := [][]byte{
		makeBotMessageEnvelope("bot-1", "C100", "BBOTID", "I am a bot", "3333.0001"),
		makeMessageEnvelope("user-1", "C100", "U200", "I am a human", "3333.0002"),
	}

	wsSrv, _ := mockWSServer(t, envelopes)
	defer wsSrv.Close()

	apiSrv := mockConnectionsOpen(t, wsSrv.URL)
	defer apiSrv.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithAPIURL(apiSrv.URL),
		WithReconnectBackoff(10*time.Millisecond, 50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	var events []*SocketEvent
	for evt := range ch {
		events = append(events, evt)
		if len(events) >= 1 {
			cancel()
		}
	}

	// Should only get the human message, bot message filtered
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1 (bot message should be filtered)", len(events))
	}
	if events[0].Text != "I am a human" {
		t.Errorf("Text = %q, want \"I am a human\"", events[0].Text)
	}
}

func TestListenDisconnectTriggersReconnect(t *testing.T) {
	// Track how many times the WS server is connected to
	var connectCount int
	var mu sync.Mutex
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		mu.Lock()
		connectCount++
		count := connectCount
		mu.Unlock()

		// Start reading acks
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		if count == 1 {
			// First connection: send disconnect
			env := makeDisconnectEnvelope("disc-1", "link_disabled")
			_ = conn.WriteMessage(websocket.TextMessage, env)
			time.Sleep(50 * time.Millisecond)
		} else {
			// Second connection: send a message then close
			env := makeMessageEnvelope("after-reconnect", "C100", "U200", "reconnected!", "4444.0001")
			_ = conn.WriteMessage(websocket.TextMessage, env)
			time.Sleep(50 * time.Millisecond)
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		}
	}))
	defer wsSrv.Close()

	apiSrv := mockConnectionsOpen(t, wsSrv.URL)
	defer apiSrv.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithAPIURL(apiSrv.URL),
		WithReconnectBackoff(10*time.Millisecond, 50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	var events []*SocketEvent
	for evt := range ch {
		events = append(events, evt)
		if len(events) >= 1 {
			cancel()
		}
	}

	// Should have received a message after reconnect
	if len(events) < 1 {
		t.Fatal("expected at least 1 event after reconnect")
	}
	if events[0].Text != "reconnected!" {
		t.Errorf("Text = %q, want \"reconnected!\"", events[0].Text)
	}

	mu.Lock()
	defer mu.Unlock()
	if connectCount < 2 {
		t.Errorf("connectCount = %d, want >= 2 (reconnect should have happened)", connectCount)
	}
}

func TestListenAcknowledgements(t *testing.T) {
	// Use a custom WS server that waits for all acks before closing.
	rec := &wsRecorder{}
	wantAcks := map[string]bool{"ack-test-1": false, "ack-test-2": false, "ack-test-3": false}
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Read acks in background
		ackCh := make(chan string, 10)
		go func() {
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					close(ackCh)
					return
				}
				rec.add(msg)
				var ack struct {
					EnvelopeID string `json:"envelope_id"`
				}
				if json.Unmarshal(msg, &ack) == nil && ack.EnvelopeID != "" {
					ackCh <- ack.EnvelopeID
				}
			}
		}()

		// Send envelopes (no disconnect — just plain events)
		envelopes := [][]byte{
			makeMessageEnvelope("ack-test-1", "C100", "U200", "message one", "5555.0001"),
			makeAppMentionEnvelope("ack-test-2", "C100", "U200", "<@UBOT> hi", "5555.0002"),
			makeMessageEnvelope("ack-test-3", "C100", "U300", "message three", "5555.0003"),
		}
		for _, env := range envelopes {
			if err := conn.WriteMessage(websocket.TextMessage, env); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}

		// Wait for all acks (with timeout)
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		received := 0
		for received < len(wantAcks) {
			select {
			case id, ok := <-ackCh:
				if !ok {
					return
				}
				if _, exists := wantAcks[id]; exists {
					received++
				}
			case <-timer.C:
				return
			}
		}

		// Close cleanly
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	}))
	defer wsSrv.Close()

	apiSrv := mockConnectionsOpen(t, wsSrv.URL)
	defer apiSrv.Close()

	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithAPIURL(apiSrv.URL),
		WithReconnectBackoff(10*time.Millisecond, 50*time.Millisecond),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen error: %v", err)
	}

	// Collect events then cancel
	var events []*SocketEvent
	for evt := range ch {
		events = append(events, evt)
		if len(events) >= 3 {
			cancel()
		}
	}

	// Verify all three envelopes were acked
	acks := rec.messages()
	ackIDs := make(map[string]bool)
	for _, msg := range acks {
		var ack struct {
			EnvelopeID string `json:"envelope_id"`
		}
		if json.Unmarshal(msg, &ack) == nil && ack.EnvelopeID != "" {
			ackIDs[ack.EnvelopeID] = true
		}
	}

	for _, wantID := range []string{"ack-test-1", "ack-test-2", "ack-test-3"} {
		if !ackIDs[wantID] {
			t.Errorf("envelope %q was not acknowledged", wantID)
		}
	}
}

// --- NewSocketModeClient tests ---

func TestNewSocketModeClient(t *testing.T) {
	tests := []struct {
		name     string
		appToken string
		opts     []SocketModeOption
	}{
		{
			name:     "default options",
			appToken: testutil.FakeSlackAppToken,
		},
		{
			name:     "with custom API URL",
			appToken: testutil.FakeSlackAppToken,
			opts:     []SocketModeOption{WithAPIURL("http://localhost:9999")},
		},
		{
			name:     "with custom backoff",
			appToken: testutil.FakeSlackAppToken,
			opts: []SocketModeOption{
				WithReconnectBackoff(100*time.Millisecond, 5*time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewSocketModeClient(tt.appToken, tt.opts...)
			if client == nil {
				t.Fatal("NewSocketModeClient returned nil")
			}
			if client.appToken != tt.appToken {
				t.Errorf("appToken = %q, want %q", client.appToken, tt.appToken)
			}
			if client.httpClient == nil {
				t.Error("httpClient is nil")
			}
		})
	}
}

// --- Backoff tests ---

func TestNextBackoff(t *testing.T) {
	client := NewSocketModeClient(testutil.FakeSlackAppToken,
		WithReconnectBackoff(1*time.Second, 30*time.Second),
	)

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
		t.Run(fmt.Sprintf("%v->%v", tt.current, tt.want), func(t *testing.T) {
			got := client.nextBackoff(tt.current)
			if got != tt.want {
				t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.want)
			}
		})
	}
}

// --- Event parsing edge cases ---

func TestParseEnvelopeWithFiles(t *testing.T) {
	payload := map[string]interface{}{
		"type": "event_callback",
		"event": map[string]interface{}{
			"type":    "message",
			"channel": "C123",
			"user":    "U456",
			"text":    "here is a file",
			"ts":      "6666.0001",
			"files": []map[string]interface{}{
				{
					"id":       "F123",
					"name":     "screenshot.png",
					"mimetype": "image/png",
					"url_private": "https://files.slack.com/files/screenshot.png",
					"size":     12345,
				},
			},
		},
	}
	env := makeEnvelope("file-1", "events_api", payload)

	_, _, event, err := parseEnvelope(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected event, got nil")
	}
	if len(event.Files) != 1 {
		t.Fatalf("got %d files, want 1", len(event.Files))
	}
	if event.Files[0].Name != "screenshot.png" {
		t.Errorf("file name = %q, want \"screenshot.png\"", event.Files[0].Name)
	}
	if event.Files[0].Size != 12345 {
		t.Errorf("file size = %d, want 12345", event.Files[0].Size)
	}
}

func TestParseEnvelopeUnknownTopLevelType(t *testing.T) {
	env := []byte(`{"envelope_id":"unk-1","type":"slash_commands","payload":{}}`)

	envID, envType, event, err := parseEnvelope(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envID != "unk-1" {
		t.Errorf("envelope_id = %q, want \"unk-1\"", envID)
	}
	if envType != "slash_commands" {
		t.Errorf("type = %q, want \"slash_commands\"", envType)
	}
	if event != nil {
		t.Errorf("expected nil event for unknown type, got %+v", event)
	}
}

func TestParseEnvelopeNonEventCallback(t *testing.T) {
	payload := map[string]interface{}{
		"type": "url_verification",
	}
	env := makeEnvelope("verify-1", "events_api", payload)

	_, _, event, err := parseEnvelope(env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil event for url_verification, got %+v", event)
	}
}

package slack

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alekspetrov/pilot/internal/testutil"
)

// TestNewSlackMessenger verifies messenger creation.
func TestNewSlackMessenger(t *testing.T) {
	client := NewClient(testutil.FakeSlackBotToken)
	messenger := NewSlackMessenger(client)

	if messenger == nil {
		t.Fatal("NewSlackMessenger returned nil")
	}
	if messenger.client != client {
		t.Error("messenger.client not set correctly")
	}
}

// TestSendText verifies plain text message sending.
func TestSendText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat.postMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var msg Message
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		if msg.Channel != "C123456" {
			t.Errorf("channel = %q, want C123456", msg.Channel)
		}
		if msg.Text != "Hello world" {
			t.Errorf("text = %q, want 'Hello world'", msg.Text)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"ts": "1234567890.000001",
		})
	}))
	defer server.Close()

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{},
	}
	client.httpClient.Transport = newTestTransport(server.URL)

	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	err := messenger.SendText(ctx, "C123456", "Hello world")
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
}

// TestSendConfirmation verifies task confirmation message with buttons.
func TestSendConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat.postMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		var msg InteractiveMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		if msg.Channel != "C123456" {
			t.Errorf("channel = %q, want C123456", msg.Channel)
		}
		if len(msg.Blocks) != 2 {
			t.Errorf("blocks count = %d, want 2", len(msg.Blocks))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"ts": "1234567890.000002",
		})
	}))
	defer server.Close()

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{},
	}
	client.httpClient.Transport = newTestTransport(server.URL)

	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	messageRef, err := messenger.SendConfirmation(ctx, "C123456", "thread-ts", "TASK-001", "Review PR changes", "myproject")
	if err != nil {
		t.Fatalf("SendConfirmation failed: %v", err)
	}
	if messageRef != "1234567890.000002" {
		t.Errorf("messageRef = %q, want 1234567890.000002", messageRef)
	}
}

// TestSendProgress verifies progress update message.
func TestSendProgress(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		// First call is update (which fails), second call is post (which succeeds)
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":    false,
				"error": "message_not_found",
			})
		} else {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok": true,
				"ts": "1234567890.000003",
			})
		}
	}))
	defer server.Close()

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{},
	}
	client.httpClient.Transport = newTestTransport(server.URL)

	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	newRef, err := messenger.SendProgress(ctx, "C123456", "1234567890.000002", "TASK-001", "planning", 50, "Analyzing requirements")
	if err != nil {
		t.Fatalf("SendProgress failed: %v", err)
	}
	if newRef != "1234567890.000003" {
		t.Errorf("newRef = %q, want 1234567890.000003", newRef)
	}
}

// TestSendResult verifies final result message.
func TestSendResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var msg InteractiveMessage
		if err := json.Unmarshal(body, &msg); err != nil {
			t.Fatalf("failed to unmarshal request: %v", err)
		}

		if msg.Channel != "C123456" {
			t.Errorf("channel = %q, want C123456", msg.Channel)
		}
		if len(msg.Blocks) == 0 {
			t.Error("blocks should not be empty")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"ts": "1234567890.000004",
		})
	}))
	defer server.Close()

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{},
	}
	client.httpClient.Transport = newTestTransport(server.URL)

	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	err := messenger.SendResult(ctx, "C123456", "thread-ts", "TASK-001", true, "Task completed successfully", "https://github.com/pr/1")
	if err != nil {
		t.Fatalf("SendResult failed: %v", err)
	}
}

// TestSendChunked verifies content chunking for long messages.
func TestSendChunked(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok": true,
			"ts": "1234567890.000005",
		})
	}))
	defer server.Close()

	client := &Client{
		botToken: testutil.FakeSlackBotToken,
		httpClient: &http.Client{},
	}
	client.httpClient.Transport = newTestTransport(server.URL)

	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	// Create content longer than MaxMessageLength
	longContent := ""
	for i := 0; i < 5000; i++ {
		longContent += "x"
	}

	err := messenger.SendChunked(ctx, "C123456", "thread-ts", longContent, "Output:")
	if err != nil {
		t.Fatalf("SendChunked failed: %v", err)
	}

	// Should have made at least 2 calls due to chunking
	if callCount < 2 {
		t.Errorf("expected at least 2 API calls, got %d", callCount)
	}
}

// TestAcknowledgeCallback verifies callback acknowledgment (should be no-op).
func TestAcknowledgeCallback(t *testing.T) {
	client := NewClient(testutil.FakeSlackBotToken)
	messenger := NewSlackMessenger(client)
	ctx := context.Background()

	err := messenger.AcknowledgeCallback(ctx, "callback-123")
	if err != nil {
		t.Fatalf("AcknowledgeCallback failed: %v", err)
	}
}

// TestMaxMessageLength verifies the maximum message length.
func TestMaxMessageLength(t *testing.T) {
	client := NewClient(testutil.FakeSlackBotToken)
	messenger := NewSlackMessenger(client)

	maxLen := messenger.MaxMessageLength()
	if maxLen != 3800 {
		t.Errorf("MaxMessageLength = %d, want 3800", maxLen)
	}
}

// newTestTransport creates an HTTP transport that redirects requests to the test server.
func newTestTransport(serverURL string) http.RoundTripper {
	return &testTransport{serverURL: serverURL}
}

type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the host with the test server host
	req.URL.Scheme = "http"
	req.URL.Host = "localhost"
	client := &http.Client{}

	// Create a new request with the test server URL
	newReq, _ := http.NewRequest(req.Method, t.serverURL+req.URL.Path, req.Body)
	newReq.Header = req.Header

	return client.Do(newReq)
}

package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alekspetrov/pilot/internal/testutil"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewClientWithBaseURL(testutil.FakeTelegramBotToken, server.URL)
	return client, server
}

func TestTelegramMessenger_SendText(t *testing.T) {
	var capturedText string
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		if text, ok := req["text"].(string); ok {
			capturedText = text
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": 1},
		})
	})
	defer server.Close()

	messenger := NewTelegramMessenger(client, true)
	err := messenger.SendText(context.Background(), "123", "hello world")
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}
	if capturedText != "hello world" {
		t.Errorf("expected text 'hello world', got '%s'", capturedText)
	}
}

func TestTelegramMessenger_MaxMessageLength(t *testing.T) {
	messenger := &TelegramMessenger{}
	if got := messenger.MaxMessageLength(); got != 4096 {
		t.Errorf("MaxMessageLength() = %d, want 4096", got)
	}
}

func TestTelegramMessenger_AcknowledgeCallback(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	})
	defer server.Close()

	messenger := NewTelegramMessenger(client, true)
	err := messenger.AcknowledgeCallback(context.Background(), "callback-123")
	if err != nil {
		t.Fatalf("AcknowledgeCallback failed: %v", err)
	}
}

func TestTelegramMessenger_SendChunked(t *testing.T) {
	var messages []string
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		_ = json.Unmarshal(body, &req)
		if text, ok := req["text"].(string); ok {
			messages = append(messages, text)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": 1},
		})
	})
	defer server.Close()

	messenger := NewTelegramMessenger(client, true)

	err := messenger.SendChunked(context.Background(), "123", "", "short text", "")
	if err != nil {
		t.Fatalf("SendChunked failed: %v", err)
	}
	if len(messages) != 1 {
		t.Errorf("expected 1 message, got %d", len(messages))
	}
	if len(messages) > 0 && messages[0] != "short text" {
		t.Errorf("expected 'short text', got '%s'", messages[0])
	}
}

func TestTelegramMessenger_SendConfirmation(t *testing.T) {
	client, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": 42},
		})
	})
	defer server.Close()

	messenger := NewTelegramMessenger(client, true)
	ref, err := messenger.SendConfirmation(context.Background(), "123", "", "TG-1", "test task", "/path")
	if err != nil {
		t.Fatalf("SendConfirmation failed: %v", err)
	}
	if ref != "42" {
		t.Errorf("expected messageRef '42', got '%s'", ref)
	}
}

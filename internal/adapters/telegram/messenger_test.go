package telegram

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
)

func TestTelegramMessenger_NewTelegramMessenger(t *testing.T) {
	client := NewClient("test-token")

	tests := []struct {
		name           string
		plainTextMode  bool
		expectedMode   string
	}{
		{
			name:           "markdown mode",
			plainTextMode:  false,
			expectedMode:   "MarkdownV2",
		},
		{
			name:           "plain text mode",
			plainTextMode:  true,
			expectedMode:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messenger := NewTelegramMessenger(client, tt.plainTextMode)
			if messenger == nil {
				t.Fatal("NewTelegramMessenger returned nil")
			}
			if messenger.getParseMode() != tt.expectedMode {
				t.Errorf("getParseMode() = %q, want %q", messenger.getParseMode(), tt.expectedMode)
			}
		})
	}
}

func TestTelegramMessenger_MaxMessageLength(t *testing.T) {
	client := NewClient("test-token")
	messenger := NewTelegramMessenger(client, false)

	if messenger.MaxMessageLength() != 4000 {
		t.Errorf("MaxMessageLength() = %d, want 4000", messenger.MaxMessageLength())
	}
}

func TestTelegramMessenger_SendText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"ok": true,
			"result": {
				"message_id": 12345,
				"chat_id": 67890,
				"date": 1234567890,
				"text": "test message"
			}
		}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	messenger := NewTelegramMessenger(client, false)

	messageRef, err := messenger.SendText("67890", "test message")
	if err != nil {
		t.Fatalf("SendText() error = %v", err)
	}

	if messageRef != "12345" {
		t.Errorf("SendText() messageRef = %q, want %q", messageRef, "12345")
	}
}

func TestTelegramMessenger_SendConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"ok": true,
			"result": {
				"message_id": 54321,
				"chat_id": 67890
			}
		}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	messenger := NewTelegramMessenger(client, false)

	messageRef, err := messenger.SendConfirmation("67890", "TASK-001", "Test task")
	if err != nil {
		t.Fatalf("SendConfirmation() error = %v", err)
	}

	if messageRef != "54321" {
		t.Errorf("SendConfirmation() messageRef = %q, want %q", messageRef, "54321")
	}
}

func TestTelegramMessenger_SendProgress(t *testing.T) {
	t.Run("new message", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"ok": true,
				"result": {
					"message_id": 99999,
					"chat_id": 67890
				}
			}`))
		}))
		defer server.Close()

		client := NewClientWithBaseURL("test-token", server.URL)
		messenger := NewTelegramMessenger(client, false)

		messageRef, err := messenger.SendProgress("67890", "TASK-001", "Implementing", 50, "Working...", nil)
		if err != nil {
			t.Fatalf("SendProgress() error = %v", err)
		}

		if messageRef != "99999" {
			t.Errorf("SendProgress() messageRef = %q, want %q", messageRef, "99999")
		}
	})

	t.Run("update existing message", func(t *testing.T) {
		callCount := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"ok": true,
				"result": {
					"message_id": 88888,
					"chat_id": 67890
				}
			}`))
		}))
		defer server.Close()

		client := NewClientWithBaseURL("test-token", server.URL)
		messenger := NewTelegramMessenger(client, false)

		existingRef := "88888"
		messageRef, err := messenger.SendProgress("67890", "TASK-001", "Testing", 75, "Running tests...", &existingRef)
		if err != nil {
			t.Fatalf("SendProgress() error = %v", err)
		}

		if messageRef != "88888" {
			t.Errorf("SendProgress() messageRef = %q, want %q", messageRef, "88888")
		}
	})
}

func TestTelegramMessenger_SendResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"ok": true,
			"result": {
				"message_id": 77777,
				"chat_id": 67890
			}
		}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	messenger := NewTelegramMessenger(client, false)

	result := &executor.ExecutionResult{
		TaskID:   "TASK-001",
		Success:  true,
		Output:   "Task completed successfully",
		Duration: 5 * time.Second,
	}

	messageRef, err := messenger.SendResult("67890", result)
	if err != nil {
		t.Fatalf("SendResult() error = %v", err)
	}

	if messageRef != "77777" {
		t.Errorf("SendResult() messageRef = %q, want %q", messageRef, "77777")
	}
}

func TestTelegramMessenger_SendChunked(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Return different message IDs for each call
		messageID := 10000 + callCount
		w.Write([]byte(fmt.Sprintf(`{
			"ok": true,
			"result": {
				"message_id": %d,
				"chat_id": 67890
			}
		}`, messageID)))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	messenger := NewTelegramMessenger(client, false)

	// Create a message longer than 4000 characters
	longMessage := ""
	for i := 0; i < 5; i++ {
		longMessage += "This is a test message that is repeated to make it longer. " // ~55 chars
	}
	for len(longMessage) < 5000 {
		longMessage += "x"
	}

	messageRefs, err := messenger.SendChunked("67890", longMessage)
	if err != nil {
		t.Fatalf("SendChunked() error = %v", err)
	}

	if len(messageRefs) < 2 {
		t.Errorf("SendChunked() should have returned at least 2 message refs, got %d", len(messageRefs))
	}
}

func TestTelegramMessenger_AcknowledgeCallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"ok": true
		}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL("test-token", server.URL)
	messenger := NewTelegramMessenger(client, false)

	err := messenger.AcknowledgeCallback("callback-id-123", "✅ Executing task...")
	if err != nil {
		t.Fatalf("AcknowledgeCallback() error = %v", err)
	}
}

func TestTelegramMessenger_MessageIDConversion(t *testing.T) {
	// Test that we properly convert between int64 message IDs and string references
	tests := []struct {
		name        string
		messageID   int64
		expectStr   string
	}{
		{
			name:        "small ID",
			messageID:   12345,
			expectStr:   "12345",
		},
		{
			name:        "large ID",
			messageID:   9223372036854775807, // max int64
			expectStr:   "9223372036854775807",
		},
		{
			name:        "zero",
			messageID:   0,
			expectStr:   "0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test conversion logic (same as used in SendText)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{
					"ok": true,
					"result": {
						"message_id": ` + tt.expectStr + `,
						"chat_id": 67890
					}
				}`))
			}))
			defer server.Close()

			client := NewClientWithBaseURL("test-token", server.URL)
			messenger := NewTelegramMessenger(client, false)

			messageRef, err := messenger.SendText("67890", "test")
			if err != nil {
				t.Fatalf("SendText() error = %v", err)
			}

			if messageRef != tt.expectStr {
				t.Errorf("messageRef = %q, want %q", messageRef, tt.expectStr)
			}
		})
	}
}

func TestTelegramMessenger_PlainTextMode(t *testing.T) {
	t.Run("plaintext mode disabled sends MarkdownV2", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// In a real scenario, we'd parse the JSON body to verify parse_mode
			// For now, we just verify the messenger was called
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"ok": true,
				"result": {
					"message_id": 11111,
					"chat_id": 67890
				}
			}`))
		}))
		defer server.Close()

		client := NewClientWithBaseURL("test-token", server.URL)
		messenger := NewTelegramMessenger(client, false)

		// Verify the parse mode
		if messenger.getParseMode() != "MarkdownV2" {
			t.Errorf("plainTextMode=false: getParseMode() = %q, want MarkdownV2", messenger.getParseMode())
		}

		_, err := messenger.SendText("67890", "test")
		if err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
	})

	t.Run("plaintext mode enabled sends empty parse mode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{
				"ok": true,
				"result": {
					"message_id": 22222,
					"chat_id": 67890
				}
			}`))
		}))
		defer server.Close()

		client := NewClientWithBaseURL("test-token", server.URL)
		messenger := NewTelegramMessenger(client, true)

		// Verify the parse mode
		if messenger.getParseMode() != "" {
			t.Errorf("plainTextMode=true: getParseMode() = %q, want empty string", messenger.getParseMode())
		}

		_, err := messenger.SendText("67890", "test")
		if err != nil {
			t.Fatalf("SendText() error = %v", err)
		}
	})
}

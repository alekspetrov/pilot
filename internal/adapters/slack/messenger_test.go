package slack

import (
	"context"
	"testing"
)

func TestSlackMessenger_MaxMessageLength(t *testing.T) {
	messenger := &SlackMessenger{}
	if got := messenger.MaxMessageLength(); got != 4000 {
		t.Errorf("MaxMessageLength() = %d, want 4000", got)
	}
}

func TestSlackMessenger_AcknowledgeCallback(t *testing.T) {
	messenger := &SlackMessenger{}
	err := messenger.AcknowledgeCallback(context.Background(), "callback-123")
	if err != nil {
		t.Fatalf("AcknowledgeCallback should be a no-op: %v", err)
	}
}

func TestNewSlackMessenger(t *testing.T) {
	client := NewClient("test-token")
	messenger := NewSlackMessenger(client)
	if messenger == nil {
		t.Fatal("NewSlackMessenger returned nil")
	}
	if messenger.client != client {
		t.Error("messenger client not set correctly")
	}
}

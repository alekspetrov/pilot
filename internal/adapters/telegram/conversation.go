package telegram

import (
	"time"

	"github.com/alekspetrov/pilot/internal/intent"
)

// ConversationStore re-exports intent.ConversationStore for backward compatibility
type ConversationStore = intent.ConversationStore

// NewConversationStore creates a new conversation history store
var NewConversationStore = intent.NewConversationStore

// ConversationStoreConfig holds configuration for conversation store
// This is a convenience wrapper for creating stores with common defaults
type ConversationStoreConfig struct {
	MaxSize int
	TTL     time.Duration
}

// DefaultConversationStoreConfig returns default configuration
func DefaultConversationStoreConfig() *ConversationStoreConfig {
	return &ConversationStoreConfig{
		MaxSize: 10,
		TTL:     30 * time.Minute,
	}
}

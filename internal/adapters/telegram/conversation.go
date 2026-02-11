package telegram

import (
	"time"

	"github.com/alekspetrov/pilot/internal/intent"
)

// ConversationStore re-exports intent.ConversationStore for backward compatibility
type ConversationStore = intent.ConversationStore

// NewConversationStore re-exports intent.NewConversationStore for backward compatibility
func NewConversationStore(maxSize int, ttl time.Duration) *ConversationStore {
	return intent.NewConversationStore(maxSize, ttl)
}

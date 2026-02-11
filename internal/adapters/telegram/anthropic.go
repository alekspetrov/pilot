package telegram

import (
	"context"

	"github.com/alekspetrov/pilot/internal/intent"
)

// ConversationMessage re-exports intent.ConversationMessage for backward compatibility
type ConversationMessage = intent.ConversationMessage

// AnthropicClient re-exports intent.AnthropicClient for backward compatibility
type AnthropicClient = intent.AnthropicClient

// ClassifyResponse re-exports intent.ClassifyResponse for backward compatibility
type ClassifyResponse = intent.ClassifyResponse

// NewAnthropicClient re-exports intent.NewAnthropicClient for backward compatibility
func NewAnthropicClient(apiKey string) *AnthropicClient {
	return intent.NewAnthropicClient(apiKey)
}

// ClassifyIntent wraps the AnthropicClient.Classify method for convenience
func ClassifyIntent(ctx context.Context, client *AnthropicClient, messages []ConversationMessage, currentMessage string) (Intent, error) {
	return client.Classify(ctx, messages, currentMessage)
}

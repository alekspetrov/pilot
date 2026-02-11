package telegram

import (
	"github.com/alekspetrov/pilot/internal/intent"
)

// AnthropicClient wraps the intent package's AnthropicClient for backward compatibility
type AnthropicClient = intent.AnthropicClient

// NewAnthropicClient creates a new Anthropic API client for intent classification
var NewAnthropicClient = intent.NewAnthropicClient

// ConversationMessage re-exports intent.ConversationMessage for backward compatibility
type ConversationMessage = intent.ConversationMessage

// ClassifyResponse re-exports intent.ClassifyResponse for backward compatibility
type ClassifyResponse = intent.ClassifyResponse

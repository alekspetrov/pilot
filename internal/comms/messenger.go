package comms

import "github.com/alekspetrov/pilot/internal/executor"

// Messenger defines the interface for sending messages through various communication channels.
type Messenger interface {
	// SendText sends a plain text message
	SendText(chatID string, text string) (string, error)

	// SendConfirmation sends a confirmation message with execute/cancel buttons
	SendConfirmation(chatID string, taskID string, description string) (string, error)

	// SendProgress sends or updates a progress message
	SendProgress(chatID string, taskID string, phase string, progress int, message string, messageRef *string) (string, error)

	// SendResult sends a task result message
	SendResult(chatID string, result *executor.ExecutionResult) (string, error)

	// SendChunked splits and sends a large message in chunks
	SendChunked(chatID string, text string) ([]string, error)

	// AcknowledgeCallback acknowledges a callback query (e.g., button press)
	AcknowledgeCallback(callbackID string, text string) error

	// MaxMessageLength returns the maximum length of a single message for this channel
	MaxMessageLength() int
}

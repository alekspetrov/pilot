// Package comms defines shared communication contracts for adapter implementations.
package comms

import (
	"context"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
)

// IncomingMessage is the platform-agnostic representation of an inbound user message.
type IncomingMessage struct {
	ContextID  string      // chatID / channelID
	SenderID   string      // user ID (string; adapters convert int64)
	Username   string      // display name of sender (for greetings)
	Text       string      // normalized message text
	ThreadID   string      // thread context (Slack threadTS, Telegram reply, etc.)
	ImagePath  string      // downloaded image path (optional)
	VoiceText  string      // transcribed voice text (optional)
	IsCallback bool        // true when this is a button callback
	CallbackID string      // platform callback ID
	ActionID   string      // button action ID
	RawEvent   interface{} // platform-specific escape hatch
}

// Messenger is the interface every chat adapter must implement for outbound messaging.
type Messenger interface {
	// SendText sends a plain text message to the given context (channel/chat).
	// threadID is optional (used by Slack for threading; empty for Telegram).
	SendText(ctx context.Context, contextID, threadID, text string) error

	// SendConfirmation sends a task confirmation prompt with approve/reject buttons.
	// Returns a messageRef that can be used to update the message later.
	SendConfirmation(ctx context.Context, contextID, threadID, taskID, desc, project string) (messageRef string, err error)

	// UpdateMessage updates a previously sent message (for progress updates).
	// msgRef is the platform-specific message reference returned by SendText/SendConfirmation.
	UpdateMessage(ctx context.Context, contextID, msgRef, text string) error

	// FormatGreeting returns a platform-specific greeting message.
	FormatGreeting(username string) string

	// FormatQuestionAck returns a platform-specific acknowledgment for questions.
	FormatQuestionAck() string

	// CleanOutput cleans internal signals from executor output.
	CleanOutput(output string) string

	// FormatTaskResult formats an execution result for display.
	FormatTaskResult(result *executor.ExecutionResult) string

	// FormatProgressUpdate formats a progress update message.
	FormatProgressUpdate(taskID, phase string, progress int, message string) string

	// MaxMessageLen returns the max message length for this platform.
	MaxMessageLen() int

	// ChunkContent splits content into platform-appropriate chunks.
	ChunkContent(content string, maxLen int) []string
}

// MemberResolver resolves a platform user to a team member ID for RBAC.
type MemberResolver interface {
	// ResolveMemberID maps a platform-specific sender ID to a team member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveMemberID(senderID string) (string, error)
}

// PendingTask represents a task awaiting user confirmation.
type PendingTask struct {
	TaskID      string
	Description string
	ContextID   string // chatID or channelID
	ThreadID    string // threadTS or empty
	MessageRef  string // platform message ID for later updates
	SenderID    string // user who requested the task (for RBAC)
	CreatedAt   time.Time
}

// RunningTask represents a task currently being executed.
type RunningTask struct {
	TaskID    string
	ContextID string
	StartedAt time.Time
	Cancel    context.CancelFunc
}

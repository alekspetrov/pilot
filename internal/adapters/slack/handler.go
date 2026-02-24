package slack

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/executor"
	"github.com/alekspetrov/pilot/internal/logging"
)

// MemberResolver resolves a Slack user to a team member ID for RBAC (GH-786).
// Decoupled from teams package to avoid import cycles.
type MemberResolver interface {
	// ResolveSlackIdentity maps a Slack user ID and/or email to a member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveSlackIdentity(slackUserID, email string) (string, error)
}

// PendingTask represents a task awaiting confirmation.
type PendingTask struct {
	TaskID      string
	Description string
	ChannelID   string
	ThreadTS    string
	MessageTS   string
	UserID      string // Slack user ID of the sender for RBAC
	CreatedAt   time.Time
}

// Handler is a thin wrapper coordinating Slack Socket Mode with task execution.
type Handler struct {
	socketClient   *SocketModeClient
	apiClient      *Client
	transport      *Transport
	runner         *executor.Runner         // Task executor (optional)
	projects       comms.ProjectSource      // Project source for multi-project support (optional)
	projectPath    string                   // Default/fallback project path
	memberResolver MemberResolver           // Team member resolver for RBAC (optional, GH-786)
	lastSender     map[string]string        // channelID -> last sender Slack user ID
	log            *slog.Logger
}

// HandlerConfig holds configuration for the Slack handler.
type HandlerConfig struct {
	AppToken        string              // Slack app-level token (xapp-...)
	BotToken        string              // Slack bot token (xoxb-...)
	MemberResolver  MemberResolver      // Team member resolver for RBAC (optional, GH-786)
	ProjectPath     string              // Default/fallback project path
	Projects        comms.ProjectSource // Project source for multi-project support
	AllowedChannels []string            // Channel IDs allowed to send tasks
	AllowedUsers    []string            // User IDs allowed to send tasks
}

// NewHandler creates a new Slack event handler with transport layer.
func NewHandler(config *HandlerConfig, runner *executor.Runner) *Handler {
	socketClient := NewSocketModeClient(config.AppToken)
	apiClient := NewClient(config.BotToken)
	transport := NewTransport(socketClient, apiClient, config.AllowedChannels, config.AllowedUsers)

	// Determine default project path
	projectPath := config.ProjectPath
	if projectPath == "" && config.Projects != nil {
		if defaultProj := config.Projects.GetDefaultProject(); defaultProj != nil {
			projectPath = defaultProj.Path
		}
	}

	h := &Handler{
		socketClient:   socketClient,
		apiClient:      apiClient,
		transport:      transport,
		runner:         runner,
		projects:       config.Projects,
		projectPath:    projectPath,
		memberResolver: config.MemberResolver,
		lastSender:     make(map[string]string),
		log:            logging.WithComponent("slack.handler"),
	}

	return h
}

// TrackSender records the user ID of the last sender in a channel.
// Called during event processing to track who sent messages for RBAC.
func (h *Handler) TrackSender(channelID, userID string) {
	if channelID == "" || userID == "" {
		return
	}
	// TODO: Add mutex if handler becomes shared
	h.lastSender[channelID] = userID
}

// resolveMemberID resolves the current Slack sender to a team member ID (GH-786).
// Returns "" if no resolver is configured or no match is found.
func (h *Handler) resolveMemberID(channelID string) string {
	if h.memberResolver == nil {
		return ""
	}

	senderID := h.lastSender[channelID]
	if senderID == "" {
		return ""
	}

	memberID, err := h.memberResolver.ResolveSlackIdentity(senderID, "")
	if err != nil {
		h.log.Warn("failed to resolve Slack identity",
			slog.String("slack_user_id", senderID),
			slog.Any("error", err))
		return ""
	}

	if memberID != "" {
		h.log.Debug("resolved Slack user to team member",
			slog.String("slack_user_id", senderID),
			slog.String("member_id", memberID))
	}

	return memberID
}

// GetLastSender returns the last sender user ID for a channel.
// Useful for testing and debugging.
func (h *Handler) GetLastSender(channelID string) string {
	return h.lastSender[channelID]
}

// isAllowed checks if a channel/user is authorized.
// This method is used by tests and delegates to transport for actual enforcement.
func (h *Handler) isAllowed(channelID, userID string) bool {
	if h.transport != nil {
		return h.transport.IsAllowed(channelID, userID)
	}
	// Fallback if transport not initialized
	return true
}

// HandleCallback processes button clicks from interactive messages (webhook endpoint).
// This is Slack-specific and stays in the handler.
func (h *Handler) HandleCallback(ctx context.Context, channelID, userID, actionID, messageTS string) {
	h.TrackSender(channelID, userID)
	h.log.Debug("Callback received",
		slog.String("channel_id", channelID),
		slog.String("action_id", actionID))
}

// StartListening starts listening for Slack Socket Mode events via the transport layer.
// It blocks until ctx is cancelled or Stop() is called.
func (h *Handler) StartListening(ctx context.Context) error {
	if h.transport == nil {
		return fmt.Errorf("transport not initialized")
	}
	return h.transport.StartListening(ctx)
}

// Stop gracefully stops the handler and transport.
func (h *Handler) Stop() {
	if h.transport != nil {
		h.transport.Stop()
	}
}

package slack

import (
	"context"
	"log/slog"
	"sync"

	"github.com/alekspetrov/pilot/internal/logging"
)

// MemberResolver resolves a Slack user to a team member ID for RBAC (GH-786).
// Decoupled from teams package to avoid import cycles.
type MemberResolver interface {
	// ResolveSlackIdentity maps a Slack user ID and/or email to a member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveSlackIdentity(slackUserID, email string) (string, error)
}

// TeamChecker checks if a member has required permissions (GH-787).
// Decoupled from teams package to avoid import cycles.
type TeamChecker interface {
	// CheckPermission verifies a member has a specific permission.
	CheckPermission(memberID string, perm string) error
}

// Handler processes incoming Slack events and coordinates task execution.
// Mirrors Telegram handler's RBAC support with memberResolver and lastSender tracking.
type Handler struct {
	client         *SocketModeClient
	usersClient    *UsersClient         // Slack users API client (optional, for email lookup)
	slackClient    *Client              // Slack API client for sending messages
	memberResolver MemberResolver       // Team member resolver for RBAC (optional, GH-786)
	teamChecker    TeamChecker          // Permission checker for RBAC (optional, GH-787)
	lastSender     map[string]string    // channelID -> last sender Slack user ID
	mu             sync.Mutex
	log            *slog.Logger
}

// HandlerConfig holds configuration for the Slack handler.
type HandlerConfig struct {
	AppToken       string         // Slack app-level token (xapp-...)
	BotToken       string         // Slack bot token (xoxb-...) for API calls
	MemberResolver MemberResolver // Team member resolver for RBAC (optional, GH-786)
	TeamChecker    TeamChecker    // Permission checker for RBAC (optional, GH-787)
}

// NewHandler creates a new Slack event handler.
func NewHandler(config *HandlerConfig) *Handler {
	h := &Handler{
		client:         NewSocketModeClient(config.AppToken),
		memberResolver: config.MemberResolver,
		teamChecker:    config.TeamChecker,
		lastSender:     make(map[string]string),
		log:            logging.WithComponent("slack.handler"),
	}

	if config.BotToken != "" {
		h.usersClient = NewUsersClient(config.BotToken)
		h.slackClient = NewClient(config.BotToken)
	}

	return h
}

// TrackSender records the user ID of the last sender in a channel.
// Called during event processing to track who sent messages for RBAC.
func (h *Handler) TrackSender(channelID, userID string) {
	if channelID == "" || userID == "" {
		return
	}
	h.mu.Lock()
	h.lastSender[channelID] = userID
	h.mu.Unlock()
}

// resolveMemberID resolves the current Slack sender to a team member ID (GH-786).
// Returns "" if no resolver is configured or no match is found.
func (h *Handler) resolveMemberID(channelID string) string {
	if h.memberResolver == nil {
		return ""
	}

	h.mu.Lock()
	senderID := h.lastSender[channelID]
	h.mu.Unlock()

	if senderID == "" {
		return ""
	}

	// Try to resolve by Slack user ID first
	memberID, err := h.memberResolver.ResolveSlackIdentity(senderID, "")
	if err != nil {
		h.log.Warn("failed to resolve Slack identity",
			slog.String("slack_user_id", senderID),
			slog.Any("error", err))
		return ""
	}

	// If found by Slack user ID, return immediately
	if memberID != "" {
		h.log.Debug("resolved Slack user to team member",
			slog.String("slack_user_id", senderID),
			slog.String("member_id", memberID))
		return memberID
	}

	// Fall back to email lookup if usersClient is available
	if h.usersClient != nil {
		userInfo, err := h.usersClient.GetUserInfo(context.Background(), senderID)
		if err != nil {
			h.log.Warn("failed to fetch Slack user info",
				slog.String("slack_user_id", senderID),
				slog.Any("error", err))
			return ""
		}

		if userInfo.Email != "" {
			memberID, err = h.memberResolver.ResolveSlackIdentity("", userInfo.Email)
			if err != nil {
				h.log.Warn("failed to resolve Slack identity by email",
					slog.String("email", userInfo.Email),
					slog.Any("error", err))
				return ""
			}
			if memberID != "" {
				h.log.Debug("resolved Slack user to team member via email",
					slog.String("slack_user_id", senderID),
					slog.String("email", userInfo.Email),
					slog.String("member_id", memberID))
				return memberID
			}
		}
	}

	return ""
}

// ResolveMemberIDForChannel resolves the Slack sender in a channel to a member ID (GH-787).
// This is the public API for RBAC integration in the execution flow.
// Returns "" if no resolver is configured or no match is found.
func (h *Handler) ResolveMemberIDForChannel(channelID string) string {
	return h.resolveMemberID(channelID)
}

// CheckTaskPermission verifies the sender has permission to execute tasks (GH-787).
// Returns nil if:
//   - No memberResolver or teamChecker is configured (RBAC disabled)
//   - Member is found and has execute_tasks permission
//
// Returns error if member is found but lacks permission.
// Sends rejection message to channel on permission denied.
func (h *Handler) CheckTaskPermission(ctx context.Context, channelID, threadTS string) error {
	if h.memberResolver == nil || h.teamChecker == nil {
		// RBAC not configured — allow all
		return nil
	}

	memberID := h.resolveMemberID(channelID)
	if memberID == "" {
		// No member mapping found — allow (soft RBAC)
		h.log.Debug("no member mapping found, allowing task",
			slog.String("channel_id", channelID))
		return nil
	}

	err := h.teamChecker.CheckPermission(memberID, "execute_tasks")
	if err != nil {
		h.log.Warn("task execution denied by RBAC",
			slog.String("member_id", memberID),
			slog.String("channel_id", channelID),
			slog.Any("error", err))

		// Send rejection message if slackClient is available
		if h.slackClient != nil {
			h.sendRejectionMessage(ctx, channelID, threadTS)
		}

		return err
	}

	h.log.Debug("RBAC check passed",
		slog.String("member_id", memberID),
		slog.String("channel_id", channelID))
	return nil
}

// sendRejectionMessage sends an unauthorized message to the channel.
func (h *Handler) sendRejectionMessage(ctx context.Context, channelID, threadTS string) {
	msg := &Message{
		Channel:  channelID,
		Text:     "You don't have permission to execute tasks. Please contact your team admin.",
		ThreadTS: threadTS,
	}

	if _, err := h.slackClient.PostMessage(ctx, msg); err != nil {
		h.log.Warn("failed to send rejection message",
			slog.String("channel_id", channelID),
			slog.Any("error", err))
	}
}

// GetLastSender returns the last sender user ID for a channel.
// Useful for testing and debugging.
func (h *Handler) GetLastSender(channelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSender[channelID]
}

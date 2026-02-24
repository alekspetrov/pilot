package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/logging"
)

// Transport handles Slack Socket Mode WebSocket connections and normalizes events.
// It extracts Slack-specific transport logic from the handler.
type Transport struct {
	socketClient    *SocketModeClient
	apiClient       *Client
	allowedChannels map[string]bool
	allowedUsers    map[string]bool
	mu              sync.Mutex
	stopCh          chan struct{}
	wg              sync.WaitGroup
	log             *slog.Logger
	// Handler receives normalized messages
	onMessage func(ctx context.Context, msg *comms.IncomingMessage) error
}

// NewTransport creates a new Slack transport.
func NewTransport(socketClient *SocketModeClient, apiClient *Client, allowedChannels, allowedUsers []string) *Transport {
	allowedChMap := make(map[string]bool)
	for _, id := range allowedChannels {
		allowedChMap[id] = true
	}

	allowedUsMap := make(map[string]bool)
	for _, id := range allowedUsers {
		allowedUsMap[id] = true
	}

	return &Transport{
		socketClient:    socketClient,
		apiClient:       apiClient,
		allowedChannels: allowedChMap,
		allowedUsers:    allowedUsMap,
		stopCh:          make(chan struct{}),
		log:             logging.WithComponent("slack.transport"),
	}
}

// SetMessageHandler sets the callback for incoming messages.
func (t *Transport) SetMessageHandler(fn func(ctx context.Context, msg *comms.IncomingMessage) error) {
	t.onMessage = fn
}

// StartListening starts listening for Slack Socket Mode events.
// It blocks until ctx is cancelled or Stop() is called.
func (t *Transport) StartListening(ctx context.Context) error {
	events, err := t.socketClient.Listen(ctx)
	if err != nil {
		return fmt.Errorf("failed to start Socket Mode listener: %w", err)
	}

	t.log.Info("Slack Socket Mode listener started")

	// Process events
	for {
		select {
		case <-ctx.Done():
			t.log.Info("Slack listener stopping (context cancelled)")
			return ctx.Err()
		case <-t.stopCh:
			t.log.Info("Slack listener stopping (stop signal)")
			return nil
		case evt, ok := <-events:
			if !ok {
				t.log.Info("Slack event channel closed")
				return nil
			}
			t.processEvent(ctx, &evt)
		}
	}
}

// Stop gracefully stops the transport.
func (t *Transport) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// processEvent handles a single Slack event and normalizes it to comms.IncomingMessage.
func (t *Transport) processEvent(ctx context.Context, event *SocketEvent) {
	// Ignore bot messages to avoid feedback loops
	if event.IsBotMessage() {
		return
	}

	channelID := event.ChannelID
	userID := event.UserID
	text := strings.TrimSpace(event.Text)

	// Security check: only process from allowed channels/users
	if !t.IsAllowed(channelID, userID) {
		t.log.Debug("Ignoring message from unauthorized channel/user",
			slog.String("channel_id", channelID),
			slog.String("user_id", userID))
		return
	}

	// Skip if no text
	if text == "" {
		return
	}

	// Normalize to platform-agnostic message format
	msg := &comms.IncomingMessage{
		ContextID: channelID,
		SenderID:  userID,
		Text:      text,
		ThreadID:  event.ThreadTS,
		RawEvent:  event,
	}

	// Call handler if registered
	if t.onMessage != nil {
		if err := t.onMessage(ctx, msg); err != nil {
			t.log.Warn("Message handler failed",
				slog.String("channel_id", channelID),
				slog.Any("error", err))
		}
	}
}

// IsAllowed checks if a channel/user is authorized.
func (t *Transport) IsAllowed(channelID, userID string) bool {
	// If no restrictions configured, allow all
	if len(t.allowedChannels) == 0 && len(t.allowedUsers) == 0 {
		return true
	}

	// Check channel allowlist
	if len(t.allowedChannels) > 0 && t.allowedChannels[channelID] {
		return true
	}

	// Check user allowlist
	if len(t.allowedUsers) > 0 && t.allowedUsers[userID] {
		return true
	}

	return false
}

package slack

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/alekspetrov/pilot/internal/comms"
	"github.com/alekspetrov/pilot/internal/logging"
)

// TransportConfig holds configuration for the Slack transport layer.
type TransportConfig struct {
	AllowedChannels []string
	AllowedUsers    []string
}

// Transport normalizes Slack Socket Mode events to comms.IncomingMessage
// and delegates to comms.Handler for platform-agnostic processing.
type Transport struct {
	socketClient    *SocketModeClient
	handler         *comms.Handler
	allowedChannels map[string]bool
	allowedUsers    map[string]bool
	stopCh          chan struct{}
	wg              sync.WaitGroup
	log             *slog.Logger
}

// NewTransport creates a new Slack transport.
func NewTransport(socketClient *SocketModeClient, handler *comms.Handler, cfg *TransportConfig) *Transport {
	allowedChannels := make(map[string]bool)
	for _, id := range cfg.AllowedChannels {
		allowedChannels[id] = true
	}

	allowedUsers := make(map[string]bool)
	for _, id := range cfg.AllowedUsers {
		allowedUsers[id] = true
	}

	return &Transport{
		socketClient:    socketClient,
		handler:         handler,
		allowedChannels: allowedChannels,
		allowedUsers:    allowedUsers,
		stopCh:          make(chan struct{}),
		log:             logging.WithComponent("slack.transport"),
	}
}

// StartListening starts listening for Slack events via Socket Mode.
// It blocks until ctx is cancelled or Stop() is called.
func (t *Transport) StartListening(ctx context.Context) error {
	events, err := t.socketClient.Listen(ctx)
	if err != nil {
		return err
	}

	t.log.Info("Slack Socket Mode transport started")

	for {
		select {
		case <-ctx.Done():
			t.log.Info("Slack transport stopping (context cancelled)")
			return ctx.Err()
		case <-t.stopCh:
			t.log.Info("Slack transport stopping (stop signal)")
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

func (t *Transport) processEvent(ctx context.Context, event *SocketEvent) {
	// Ignore bot messages to avoid feedback loops
	if event.IsBotMessage() {
		return
	}

	channelID := event.ChannelID
	userID := event.UserID
	text := strings.TrimSpace(event.Text)

	// Security check
	if !t.isAllowed(channelID, userID) {
		t.log.Debug("Ignoring message from unauthorized channel/user",
			slog.String("channel_id", channelID),
			slog.String("user_id", userID))
		return
	}

	if text == "" {
		return
	}

	// Normalize to IncomingMessage and delegate to comms.Handler
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: channelID,
		SenderID:  userID,
		Text:      text,
		ThreadID:  event.ThreadTS,
		RawEvent:  event,
	})
}

func (t *Transport) isAllowed(channelID, userID string) bool {
	if len(t.allowedChannels) == 0 && len(t.allowedUsers) == 0 {
		return true
	}
	if len(t.allowedChannels) > 0 && t.allowedChannels[channelID] {
		return true
	}
	if len(t.allowedUsers) > 0 && t.allowedUsers[userID] {
		return true
	}
	return false
}

// HandleCallback processes button clicks from interactive messages.
// Called by the webhook handler when Slack sends an interaction payload.
func (t *Transport) HandleCallback(ctx context.Context, channelID, userID, actionID, messageTS string) {
	t.handler.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID:  channelID,
		SenderID:   userID,
		IsCallback: true,
		ActionID:   actionID,
		RawEvent:   nil,
	})
}

// SlackMemberResolverAdapter wraps the Slack-specific MemberResolver to
// satisfy the generic comms.MemberResolver interface.
type SlackMemberResolverAdapter struct {
	resolver MemberResolver
}

// NewSlackMemberResolverAdapter creates a new adapter.
func NewSlackMemberResolverAdapter(resolver MemberResolver) *SlackMemberResolverAdapter {
	return &SlackMemberResolverAdapter{resolver: resolver}
}

// ResolveIdentity implements comms.MemberResolver by delegating to the
// Slack-specific resolver with the senderID as a Slack user ID string.
func (a *SlackMemberResolverAdapter) ResolveIdentity(senderID string) (string, error) {
	if a.resolver == nil || senderID == "" {
		return "", nil
	}
	return a.resolver.ResolveSlackIdentity(senderID, "")
}

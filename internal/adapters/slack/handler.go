package slack

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
	"github.com/alekspetrov/pilot/internal/logging"
)

// MemberResolver resolves a Slack user to a team member ID for RBAC.
// Decoupled from teams package to avoid import cycles.
type MemberResolver interface {
	// ResolveSlackIdentity maps a Slack user ID and/or email to a member ID.
	// Returns ("", nil) when no match is found (= skip RBAC).
	ResolveSlackIdentity(slackUserID string, email string) (string, error)
}

// PendingTask represents a task awaiting confirmation
type PendingTask struct {
	TaskID      string
	Description string
	ChannelID   string
	ThreadTS    string    // Thread timestamp for conversation context
	UserID      string    // Slack user ID of the sender
	CreatedAt   time.Time
}

// RunningTask represents a task currently being executed
type RunningTask struct {
	TaskID    string
	ChannelID string
	ThreadTS  string // Thread where task updates are posted
	StartedAt time.Time
	Cancel    context.CancelFunc
}

// ConversationStore maintains recent message history per channel/thread
type ConversationStore struct {
	mu       sync.RWMutex
	history  map[string][]ConversationMessage // channelID:threadTS -> messages
	maxSize  int
	ttl      time.Duration
	lastSeen map[string]time.Time
}

// ConversationMessage represents a message in conversation history
type ConversationMessage struct {
	Role      string // "user" or "assistant"
	Content   string
	UserID    string
	Timestamp time.Time
}

// NewConversationStore creates a new conversation history store
func NewConversationStore(maxSize int, ttl time.Duration) *ConversationStore {
	store := &ConversationStore{
		history:  make(map[string][]ConversationMessage),
		maxSize:  maxSize,
		ttl:      ttl,
		lastSeen: make(map[string]time.Time),
	}

	// Start cleanup goroutine
	go store.cleanupLoop()

	return store
}

// Add adds a message to the conversation history
func (s *ConversationStore) Add(channelID, threadTS, role, content, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeConversationKey(channelID, threadTS)
	s.history[key] = append(s.history[key], ConversationMessage{
		Role:      role,
		Content:   content,
		UserID:    userID,
		Timestamp: time.Now(),
	})

	// Trim to max size
	if len(s.history[key]) > s.maxSize {
		s.history[key] = s.history[key][len(s.history[key])-s.maxSize:]
	}

	s.lastSeen[key] = time.Now()
}

// Get returns the conversation history for a channel/thread
func (s *ConversationStore) Get(channelID, threadTS string) []ConversationMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := makeConversationKey(channelID, threadTS)
	if msgs, ok := s.history[key]; ok {
		// Return a copy
		result := make([]ConversationMessage, len(msgs))
		copy(result, msgs)
		return result
	}
	return nil
}

// cleanupLoop removes stale conversations
func (s *ConversationStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for key, lastSeen := range s.lastSeen {
			if now.Sub(lastSeen) > s.ttl {
				delete(s.history, key)
				delete(s.lastSeen, key)
			}
		}
		s.mu.Unlock()
	}
}

// makeConversationKey creates a unique key for channel+thread
func makeConversationKey(channelID, threadTS string) string {
	if threadTS == "" {
		return channelID
	}
	return channelID + ":" + threadTS
}

// HandlerConfig holds configuration for the Slack handler
type HandlerConfig struct {
	// BotToken is the Slack bot token (xoxb-...)
	BotToken string

	// AppToken is the Slack app-level token (xapp-...) for Socket Mode
	AppToken string

	// ProjectPath is the default/fallback project path
	ProjectPath string

	// Projects is the project source for multi-project support
	Projects ProjectSource

	// AllowedUsers is a list of Slack user IDs allowed to send tasks (empty = allow all)
	AllowedUsers []string

	// AllowedChannels is a list of channel IDs allowed for task execution (empty = allow all)
	AllowedChannels []string

	// RateLimit is the rate limiting configuration
	RateLimit *RateLimitConfig

	// MemberResolver resolves Slack users to team member IDs for RBAC (optional)
	MemberResolver MemberResolver
}

// Handler processes incoming Slack messages and executes tasks
type Handler struct {
	socketClient      *SocketModeClient
	apiClient         *Client
	runner            *executor.Runner
	projects          ProjectSource
	projectPath       string
	activeProject     map[string]string       // channelID -> projectPath
	allowedUsers      map[string]bool         // Allowed user IDs (empty = allow all)
	allowedChannels   map[string]bool         // Allowed channel IDs (empty = allow all)
	pendingTasks      map[string]*PendingTask // channelID:threadTS -> pending task
	runningTasks      map[string]*RunningTask // channelID:threadTS -> running task
	conversationStore *ConversationStore
	rateLimiter       *RateLimiter
	memberResolver    MemberResolver
	mu                sync.Mutex
	stopCh            chan struct{}
	wg                sync.WaitGroup
	log               *slog.Logger
}

// NewHandler creates a new Slack message handler
func NewHandler(config *HandlerConfig, runner *executor.Runner) *Handler {
	// Build allowed users map
	allowedUsers := make(map[string]bool)
	for _, id := range config.AllowedUsers {
		allowedUsers[id] = true
	}

	// Build allowed channels map
	allowedChannels := make(map[string]bool)
	for _, id := range config.AllowedChannels {
		allowedChannels[id] = true
	}

	// Determine default project path
	projectPath := config.ProjectPath
	if projectPath == "" && config.Projects != nil {
		projects := config.Projects.ListProjects()
		if len(projects) > 0 {
			projectPath = projects[0].WorkDir
		}
	}

	// Initialize rate limiter
	var rateLimiter *RateLimiter
	if config.RateLimit != nil {
		rateLimiter = NewRateLimiter(config.RateLimit)
	} else {
		rateLimiter = NewRateLimiter(DefaultRateLimitConfig())
	}

	// Initialize conversation store (10 messages, 1 hour TTL)
	conversationStore := NewConversationStore(10, time.Hour)

	return &Handler{
		socketClient:      NewSocketModeClient(config.AppToken),
		apiClient:         NewClient(config.BotToken),
		runner:            runner,
		projects:          config.Projects,
		projectPath:       projectPath,
		activeProject:     make(map[string]string),
		allowedUsers:      allowedUsers,
		allowedChannels:   allowedChannels,
		pendingTasks:      make(map[string]*PendingTask),
		runningTasks:      make(map[string]*RunningTask),
		conversationStore: conversationStore,
		rateLimiter:       rateLimiter,
		memberResolver:    config.MemberResolver,
		stopCh:            make(chan struct{}),
		log:               logging.WithComponent("slack.handler"),
	}
}

// StartListening connects to Slack Socket Mode and processes events
func (h *Handler) StartListening(ctx context.Context) error {
	h.log.Info("Starting Slack Socket Mode listener")

	// Connect to Socket Mode
	events, err := h.socketClient.Listen(ctx)
	if err != nil {
		return err
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.eventLoop(ctx, events)
	}()

	return nil
}

// Stop gracefully stops the handler
func (h *Handler) Stop() {
	h.log.Info("Stopping Slack handler")

	// Signal stop
	close(h.stopCh)

	// Cancel all running tasks
	h.mu.Lock()
	for _, task := range h.runningTasks {
		if task.Cancel != nil {
			task.Cancel()
		}
	}
	h.mu.Unlock()

	// Wait for goroutines
	h.wg.Wait()

	h.log.Info("Slack handler stopped")
}

// eventLoop processes events from the Socket Mode channel
func (h *Handler) eventLoop(ctx context.Context, events <-chan SocketEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case evt, ok := <-events:
			if !ok {
				h.log.Info("Event channel closed")
				return
			}
			h.processEvent(ctx, &evt)
		}
	}
}

// processEvent handles a single Socket Mode event with security checks
func (h *Handler) processEvent(ctx context.Context, evt *SocketEvent) {
	// Filter bot messages to prevent feedback loops
	if evt.IsBotMessage() {
		h.log.Debug("Ignoring bot message", slog.String("bot_id", evt.BotID))
		return
	}

	// Security check: allowed users
	if len(h.allowedUsers) > 0 && !h.allowedUsers[evt.UserID] {
		h.log.Warn("Unauthorized user",
			slog.String("user_id", evt.UserID),
			slog.String("channel_id", evt.ChannelID))
		return
	}

	// Security check: allowed channels
	if len(h.allowedChannels) > 0 && !h.allowedChannels[evt.ChannelID] {
		h.log.Debug("Ignoring message from non-allowed channel",
			slog.String("channel_id", evt.ChannelID))
		return
	}

	// Rate limiting
	if !h.rateLimiter.AllowMessage(evt.ChannelID) {
		h.log.Warn("Rate limited",
			slog.String("channel_id", evt.ChannelID),
			slog.String("user_id", evt.UserID))
		return
	}

	// DM vs channel logic:
	// - DMs (channel starts with 'D'): process all messages
	// - Channels: require app_mention event type
	isDM := len(evt.ChannelID) > 0 && evt.ChannelID[0] == 'D'
	if !isDM && evt.Type != EventTypeAppMention {
		h.log.Debug("Ignoring non-mention message in channel",
			slog.String("type", evt.Type),
			slog.String("channel_id", evt.ChannelID))
		return
	}

	h.log.Info("Processing event",
		slog.String("type", evt.Type),
		slog.String("channel_id", evt.ChannelID),
		slog.String("user_id", evt.UserID),
		slog.String("text", truncateText(evt.Text, 50)))

	// Store in conversation history
	h.conversationStore.Add(evt.ChannelID, evt.ThreadTS, "user", evt.Text, evt.UserID)

	// Route to appropriate handler based on intent
	h.handleMessage(ctx, evt)
}

// handleMessage routes the message to the appropriate handler
func (h *Handler) handleMessage(ctx context.Context, evt *SocketEvent) {
	// Get effective project path for this channel
	projectPath := h.getProjectPath(evt.ChannelID)

	h.log.Debug("Handling message",
		slog.String("channel_id", evt.ChannelID),
		slog.String("project_path", projectPath),
		slog.String("text", evt.Text))

	// For now, acknowledge the message - full task execution will be wired in later
	// This establishes the handler infrastructure
	h.sendAck(ctx, evt)
}

// getProjectPath returns the project path for a channel
func (h *Handler) getProjectPath(channelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if there's an active project for this channel
	if path, ok := h.activeProject[channelID]; ok {
		return path
	}

	// Check project source
	if h.projects != nil {
		if proj, err := h.projects.GetProject(channelID); err == nil && proj != nil {
			return proj.WorkDir
		}
	}

	// Fall back to default
	return h.projectPath
}

// sendAck sends an acknowledgment message back to the channel
func (h *Handler) sendAck(ctx context.Context, evt *SocketEvent) {
	msg := &Message{
		Channel:  evt.ChannelID,
		Text:     "Message received. Task execution coming soon.",
		ThreadTS: evt.ThreadTS,
	}

	// If this is a new message (not in a thread), use the message timestamp as thread
	if msg.ThreadTS == "" {
		msg.ThreadTS = evt.Timestamp
	}

	if _, err := h.apiClient.PostMessage(ctx, msg); err != nil {
		h.log.Error("Failed to send acknowledgment",
			slog.String("channel_id", evt.ChannelID),
			slog.Any("error", err))
	}
}

// truncateText truncates text to maxLen characters with ellipsis
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

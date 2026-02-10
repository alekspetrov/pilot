package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// handleMessage routes the message to the appropriate handler based on intent
func (h *Handler) handleMessage(ctx context.Context, evt *SocketEvent) {
	// Get effective thread for replies
	threadTS := evt.ThreadTS
	if threadTS == "" {
		threadTS = evt.Timestamp
	}

	// Detect intent
	intent := DetectIntent(evt.Text)

	h.log.Info("Detected intent",
		slog.String("channel_id", evt.ChannelID),
		slog.String("intent", string(intent)),
		slog.String("text", truncateText(evt.Text, 50)))

	// Route to appropriate handler
	switch intent {
	case IntentGreeting:
		h.handleGreeting(ctx, evt.ChannelID, threadTS)
	case IntentChat:
		h.handleChat(ctx, evt.ChannelID, threadTS, evt.Text)
	case IntentQuestion:
		h.handleQuestion(ctx, evt.ChannelID, threadTS, evt.Text)
	case IntentResearch:
		h.handleResearch(ctx, evt.ChannelID, threadTS, evt.Text)
	case IntentPlanning:
		h.handlePlanning(ctx, evt.ChannelID, threadTS, evt.Text)
	case IntentTask:
		h.handleTask(ctx, evt.ChannelID, threadTS, evt.Text, evt.UserID)
	case IntentCommand:
		h.handleCommand(ctx, evt.ChannelID, threadTS, evt.Text)
	default:
		// Unknown intent, acknowledge and suggest help
		h.sendReply(ctx, evt.ChannelID, threadTS,
			"I'm not sure what you're asking. Try `/help` for available commands.")
	}
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

// sendReply sends a reply message to a thread
func (h *Handler) sendReply(ctx context.Context, channelID, threadTS, text string) {
	msg := &Message{
		Channel:  channelID,
		Text:     text,
		ThreadTS: threadTS,
	}
	if _, err := h.apiClient.PostMessage(ctx, msg); err != nil {
		h.log.Error("Failed to send reply",
			slog.String("channel_id", channelID),
			slog.Any("error", err))
	}
}

// sendBlocksReply sends a Block Kit message reply to a thread
func (h *Handler) sendBlocksReply(ctx context.Context, channelID, threadTS string, blocks []Block, fallbackText string) {
	msg := &Message{
		Channel:  channelID,
		Blocks:   blocks,
		Text:     fallbackText,
		ThreadTS: threadTS,
	}
	if _, err := h.apiClient.PostMessage(ctx, msg); err != nil {
		h.log.Error("Failed to send blocks reply",
			slog.String("channel_id", channelID),
			slog.Any("error", err))
	}
}

// handleGreeting responds to greetings with a welcome message
func (h *Handler) handleGreeting(ctx context.Context, channelID, threadTS string) {
	// Get user info for personalized greeting (empty for now)
	blocks := FormatGreeting("")
	h.sendBlocksReply(ctx, channelID, threadTS, blocks, "Hey! I'm Pilot.")
}

// handleChat handles conversational messages
// Responds conversationally with a 60s timeout
func (h *Handler) handleChat(ctx context.Context, channelID, threadTS, text string) {
	// Send typing indicator
	h.sendReply(ctx, channelID, threadTS, ":speech_balloon: Thinking...")

	// Get project path for context
	projectPath := h.getProjectPath(channelID)

	// Create chat task (read-only, conversational)
	taskID := fmt.Sprintf("CHAT-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Chat: " + truncateText(text, 30),
		Description: fmt.Sprintf(`You are Pilot, an AI assistant for the codebase at %s.

The user wants to have a conversation (not execute a task).
Respond helpfully and conversationally. You can reference project knowledge but DO NOT make code changes.

Be concise - this is a chat conversation, not a report. Keep response under 500 words.

User message: %s`, projectPath, text),
		ProjectPath: projectPath,
		CreatePR:    false,
	}

	// Execute with 60 second timeout
	chatCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	h.log.Debug("Chat response", slog.String("task_id", taskID), slog.String("channel_id", channelID))
	result, err := h.runner.Execute(chatCtx, task)

	if err != nil {
		if chatCtx.Err() == context.DeadlineExceeded {
			h.sendReply(ctx, channelID, threadTS, ":hourglass: Took too long to respond. Try a simpler question.")
		} else {
			h.sendReply(ctx, channelID, threadTS, "Sorry, I couldn't process that. Try rephrasing?")
		}
		return
	}

	// Clean and send response
	response := cleanInternalSignals(result.Output)
	if response == "" {
		response = "I'm not sure how to respond to that. Could you rephrase?"
	}

	// Truncate if too long for Slack
	if len(response) > 3000 {
		response = response[:2997] + "..."
	}

	blocks := FormatQuestionAnswer(response)
	h.sendBlocksReply(ctx, channelID, threadTS, blocks, response)

	// Record in conversation history
	h.conversationStore.Add(channelID, threadTS, "assistant", truncateText(response, 500), "")
}

// handleQuestion handles questions about the codebase
// Read-only Claude execution with 90s timeout
func (h *Handler) handleQuestion(ctx context.Context, channelID, threadTS, text string) {
	// Send acknowledgment
	h.sendReply(ctx, channelID, threadTS, ":mag: Looking into that...")

	// Get project path
	projectPath := h.getProjectPath(channelID)

	// Create a read-only prompt for Claude
	prompt := fmt.Sprintf(`Answer this question about the codebase. DO NOT make any changes, only read and analyze.

Question: %s

IMPORTANT: Be concise. Limit your exploration to 5-10 files max. Provide a brief, direct answer.
If the question is too broad, ask for clarification instead of exploring everything.`, text)

	// Create a read-only task (no branch, no PR)
	taskID := fmt.Sprintf("Q-%d", time.Now().Unix())
	task := &executor.Task{
		ID:          taskID,
		Title:       "Question: " + truncateText(text, 40),
		Description: prompt,
		ProjectPath: projectPath,
		CreatePR:    false,
	}

	// Execute with 90 second timeout
	questionCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	h.log.Debug("Answering question", slog.String("task_id", taskID), slog.String("channel_id", channelID))
	result, err := h.runner.Execute(questionCtx, task)

	if err != nil {
		if questionCtx.Err() == context.DeadlineExceeded {
			h.sendReply(ctx, channelID, threadTS, ":hourglass: Question timed out. Try asking something more specific.")
		} else {
			h.sendReply(ctx, channelID, threadTS, ":x: Sorry, I couldn't answer that question. Try rephrasing it.")
		}
		return
	}

	// Format and send answer
	answer := cleanInternalSignals(result.Output)
	if answer == "" {
		answer = "I couldn't find a clear answer to that question."
	}

	blocks := FormatQuestionAnswer(answer)
	h.sendBlocksReply(ctx, channelID, threadTS, blocks, answer)
}

// handleResearch handles deep research/analysis requests
// Saves results to .agent/research/ and posts summary to thread
func (h *Handler) handleResearch(ctx context.Context, channelID, threadTS, text string) {
	// Send acknowledgment
	h.sendReply(ctx, channelID, threadTS, ":microscope: Researching...")

	// Get project path
	projectPath := h.getProjectPath(channelID)

	// Create research task
	taskID := fmt.Sprintf("RES-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Research: " + truncateText(text, 40),
		Description: fmt.Sprintf(`Research and analyze: %s

Provide findings in a structured format with:
- Executive summary
- Key findings
- Relevant code/files if applicable
- Recommendations

DO NOT make any code changes. This is a read-only research task.`, text),
		ProjectPath: projectPath,
		CreatePR:    false,
	}

	// Execute with 3 minute timeout
	researchCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	h.log.Info("Executing research", slog.String("task_id", taskID), slog.String("channel_id", channelID))
	result, err := h.runner.Execute(researchCtx, task)

	if err != nil {
		if researchCtx.Err() == context.DeadlineExceeded {
			h.sendReply(ctx, channelID, threadTS, ":hourglass: Research timed out. Try a more specific query.")
		} else {
			h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":x: Research failed: %s", err.Error()))
		}
		return
	}

	// Process and send results
	h.sendResearchOutput(ctx, channelID, threadTS, text, result, projectPath)
}

// sendResearchOutput sends research findings to Slack and saves to file
func (h *Handler) sendResearchOutput(ctx context.Context, channelID, threadTS, query string, result *executor.ExecutionResult, projectPath string) {
	content := cleanInternalSignals(result.Output)
	if content == "" {
		h.sendReply(ctx, channelID, threadTS, ":person_shrugging: No research findings to report.")
		return
	}

	// Chunk content for Slack (3000 char limit per block)
	chunks := chunkContent(content, 2800)

	for i, chunk := range chunks {
		var header string
		if len(chunks) > 1 {
			header = fmt.Sprintf(":page_facing_up: Part %d/%d\n\n", i+1, len(chunks))
		}
		blocks := FormatQuestionAnswer(header + chunk)
		h.sendBlocksReply(ctx, channelID, threadTS, blocks, chunk)

		// Small delay between chunks
		if i < len(chunks)-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}

	// Save to file
	filePath := h.saveResearchFile(projectPath, query, content)
	if filePath != "" {
		h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":floppy_disk: Saved to `%s`", filePath))
	}
}

// saveResearchFile saves research output to .agent/research/ directory
func (h *Handler) saveResearchFile(projectPath, query, content string) string {
	// Create .agent/research/ directory if it doesn't exist
	researchDir := filepath.Join(projectPath, ".agent", "research")
	if err := os.MkdirAll(researchDir, 0755); err != nil {
		return ""
	}

	// Generate filename from query
	slug := strings.ToLower(query)
	slug = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(slug, "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	filename := fmt.Sprintf("%s-%d.md", slug, time.Now().Unix())

	// Save file
	filePath := filepath.Join(researchDir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return ""
	}

	h.log.Debug("Saved research file", slog.String("path", filePath))
	return filepath.Join(".agent", "research", filename)
}

// handlePlanning handles planning/design requests
// Creates a plan and presents Execute/Cancel buttons for user approval
func (h *Handler) handlePlanning(ctx context.Context, channelID, threadTS, text string) {
	// Send acknowledgment
	h.sendReply(ctx, channelID, threadTS, ":triangular_ruler: Drafting plan...")

	// Get project path
	projectPath := h.getProjectPath(channelID)

	// Create planning task
	taskID := fmt.Sprintf("PLAN-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Plan: " + truncateText(text, 40),
		Description: fmt.Sprintf(`Create an implementation plan for: %s

Explore the codebase and propose a detailed plan. Include:
1. Summary of approach
2. Files to modify/create
3. Step-by-step implementation phases
4. Potential risks or considerations

DO NOT make any code changes. Only explore and plan.`, text),
		ProjectPath: projectPath,
		CreatePR:    false,
	}

	// Execute with 2 minute timeout
	planCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	h.log.Info("Creating plan", slog.String("task_id", taskID), slog.String("channel_id", channelID))
	result, err := h.runner.Execute(planCtx, task)

	if err != nil {
		if planCtx.Err() == context.DeadlineExceeded {
			h.sendReply(ctx, channelID, threadTS, ":hourglass: Planning timed out. Try a simpler request.")
		} else {
			h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":x: Planning failed: %s", err.Error()))
		}
		return
	}

	planContent := cleanInternalSignals(result.Output)
	if planContent == "" {
		h.sendReply(ctx, channelID, threadTS, ":person_shrugging: Could not generate a plan. Try being more specific.")
		return
	}

	// Store as pending task for potential execution (before sending buttons)
	h.mu.Lock()
	key := makeConversationKey(channelID, threadTS)
	h.pendingTasks[key] = &PendingTask{
		TaskID:      taskID,
		Description: fmt.Sprintf("## Implementation Plan\n\n%s\n\n## Original Request\n\n%s", planContent, text),
		ChannelID:   channelID,
		ThreadTS:    threadTS,
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	// Send plan summary with Execute/Cancel buttons
	summary := extractPlanSummary(planContent)
	blocks := FormatPlanWithActions(taskID, summary)

	msg := &InteractiveMessage{
		Channel: channelID,
		Text:    "Implementation Plan",
		Blocks:  blocks,
	}
	if threadTS != "" {
		// Add thread_ts for threading - InteractiveMessage doesn't have it, use raw post
		h.sendInteractiveReply(ctx, channelID, threadTS, blocks, "Implementation Plan")
	} else {
		if _, err := h.apiClient.PostInteractiveMessage(ctx, msg); err != nil {
			h.log.Error("Failed to send plan with buttons",
				slog.String("channel_id", channelID),
				slog.Any("error", err))
			// Fall back to plain message
			plainBlocks := FormatQuestionAnswer(":clipboard: *Implementation Plan*\n\n" + summary)
			h.sendBlocksReply(ctx, channelID, threadTS, plainBlocks, "Implementation Plan")
		}
	}
}

// extractPlanSummary extracts key points from a plan for display
func extractPlanSummary(plan string) string {
	lines := strings.Split(plan, "\n")
	var summary []string
	lineCount := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		summary = append(summary, trimmed)
		lineCount++

		if lineCount >= 20 {
			break
		}
	}

	result := strings.Join(summary, "\n")
	if len(result) > 2000 {
		result = result[:2000] + "\n\n_(truncated)_"
	}

	return result
}

// handleTask handles task requests with confirmation flow
// Presents a confirmation message with Execute/Cancel buttons before executing
func (h *Handler) handleTask(ctx context.Context, channelID, threadTS, text, userID string) {
	// Generate task ID
	taskID := fmt.Sprintf("TASK-%d", time.Now().Unix())

	// Store as pending task
	h.mu.Lock()
	key := makeConversationKey(channelID, threadTS)
	h.pendingTasks[key] = &PendingTask{
		TaskID:      taskID,
		Description: text,
		ChannelID:   channelID,
		ThreadTS:    threadTS,
		UserID:      userID,
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	h.log.Info("Created pending task",
		slog.String("task_id", taskID),
		slog.String("channel_id", channelID),
		slog.String("user_id", userID))

	// Send confirmation message with Execute/Cancel buttons
	blocks := FormatTaskConfirmation(taskID, text)
	h.sendInteractiveReply(ctx, channelID, threadTS, blocks, "Confirm task execution")
}

// handleCommand handles slash commands
func (h *Handler) handleCommand(ctx context.Context, channelID, threadTS, text string) {
	// Parse and execute command
	cmd := ParseCommand(text)
	response := h.executeCommand(ctx, channelID, cmd)
	h.sendReply(ctx, channelID, threadTS, response)
}

// executeCommand executes a parsed command and returns the response
func (h *Handler) executeCommand(ctx context.Context, channelID string, cmd *Command) string {
	if cmd == nil {
		return "Unknown command. Try `/help` for available commands."
	}

	switch cmd.Name {
	case "help":
		return formatHelpMessage()
	case "status":
		return h.getStatusMessage(channelID)
	case "queue":
		return h.getQueueMessage(channelID)
	case "switch":
		return h.switchProject(channelID, cmd.Args)
	case "cancel":
		return h.cancelRunningTask(ctx, channelID, cmd.Args)
	default:
		return fmt.Sprintf("Unknown command: `%s`. Try `/help` for available commands.", cmd.Name)
	}
}

// getStatusMessage returns the current status for a channel
func (h *Handler) getStatusMessage(channelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(":bar_chart: *Pilot Status*\n\n")

	// Current project
	projectPath := h.projectPath
	if path, ok := h.activeProject[channelID]; ok {
		projectPath = path
	}
	sb.WriteString(fmt.Sprintf("*Project:* `%s`\n", filepath.Base(projectPath)))

	// Running tasks count
	runningCount := 0
	for _, task := range h.runningTasks {
		if task.ChannelID == channelID {
			runningCount++
		}
	}
	sb.WriteString(fmt.Sprintf("*Running tasks:* %d\n", runningCount))

	// Pending tasks count
	pendingCount := 0
	for key := range h.pendingTasks {
		if strings.HasPrefix(key, channelID) {
			pendingCount++
		}
	}
	sb.WriteString(fmt.Sprintf("*Pending tasks:* %d\n", pendingCount))

	return sb.String()
}

// getQueueMessage returns the task queue for a channel
func (h *Handler) getQueueMessage(channelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(":clipboard: *Task Queue*\n\n")

	// Running tasks
	hasItems := false
	for _, task := range h.runningTasks {
		if task.ChannelID == channelID {
			elapsed := time.Since(task.StartedAt).Round(time.Second)
			sb.WriteString(fmt.Sprintf(":gear: *Running:* `%s` (%s elapsed)\n", task.TaskID, elapsed))
			hasItems = true
		}
	}

	// Pending tasks
	for key, task := range h.pendingTasks {
		if strings.HasPrefix(key, channelID) {
			age := time.Since(task.CreatedAt).Round(time.Second)
			sb.WriteString(fmt.Sprintf(":hourglass: *Pending:* `%s` (%s ago)\n", task.TaskID, age))
			hasItems = true
		}
	}

	if !hasItems {
		sb.WriteString("_No tasks in queue_")
	}

	return sb.String()
}

// switchProject switches the active project for a channel
func (h *Handler) switchProject(channelID string, args string) string {
	projectName := strings.TrimSpace(args)
	if projectName == "" {
		// List available projects
		if h.projects == nil {
			return fmt.Sprintf(":file_folder: Current project: `%s`\n\nNo project source configured for switching.", filepath.Base(h.projectPath))
		}

		projects := h.projects.ListProjects()
		if len(projects) == 0 {
			return ":warning: No projects available."
		}

		var sb strings.Builder
		sb.WriteString(":file_folder: *Available Projects*\n\n")
		for _, p := range projects {
			sb.WriteString(fmt.Sprintf("• `%s` - %s\n", p.Name, p.WorkDir))
		}
		sb.WriteString("\nUse `/switch <project-name>` to switch.")
		return sb.String()
	}

	// Find and switch to project
	if h.projects == nil {
		return ":warning: No project source configured."
	}

	projects := h.projects.ListProjects()
	for _, p := range projects {
		if strings.EqualFold(p.Name, projectName) {
			h.mu.Lock()
			h.activeProject[channelID] = p.WorkDir
			h.mu.Unlock()
			return fmt.Sprintf(":white_check_mark: Switched to project `%s`\nPath: `%s`", p.Name, p.WorkDir)
		}
	}

	return fmt.Sprintf(":x: Project `%s` not found. Use `/switch` to list available projects.", projectName)
}

// cancelRunningTask cancels a running task
func (h *Handler) cancelRunningTask(ctx context.Context, channelID string, args string) string {
	taskID := strings.TrimSpace(args)

	h.mu.Lock()
	defer h.mu.Unlock()

	// If no task ID specified, cancel the most recent running task in this channel
	if taskID == "" {
		for key, task := range h.runningTasks {
			if task.ChannelID == channelID && task.Cancel != nil {
				task.Cancel()
				delete(h.runningTasks, key)
				return fmt.Sprintf(":stop_sign: Cancelled task `%s`", task.TaskID)
			}
		}
		return ":info_source: No running tasks to cancel."
	}

	// Find and cancel specific task
	for key, task := range h.runningTasks {
		if task.TaskID == taskID && task.ChannelID == channelID {
			if task.Cancel != nil {
				task.Cancel()
			}
			delete(h.runningTasks, key)
			return fmt.Sprintf(":stop_sign: Cancelled task `%s`", taskID)
		}
	}

	return fmt.Sprintf(":x: Task `%s` not found or not running.", taskID)
}

// sendInteractiveReply sends a Block Kit message with interactive elements
func (h *Handler) sendInteractiveReply(ctx context.Context, channelID, threadTS string, blocks []interface{}, fallbackText string) {
	// Build payload manually to include thread_ts
	payload := struct {
		Channel  string        `json:"channel"`
		Text     string        `json:"text,omitempty"`
		Blocks   []interface{} `json:"blocks,omitempty"`
		ThreadTS string        `json:"thread_ts,omitempty"`
	}{
		Channel:  channelID,
		Text:     fallbackText,
		Blocks:   blocks,
		ThreadTS: threadTS,
	}

	if err := h.apiClient.postJSON(ctx, "/chat.postMessage", payload); err != nil {
		h.log.Error("Failed to send interactive reply",
			slog.String("channel_id", channelID),
			slog.Any("error", err))
	}
}

// executeTask executes a confirmed task with progress updates
func (h *Handler) executeTask(ctx context.Context, channelID, threadTS, taskID, description string) {
	// Check if runner is available
	if h.runner == nil {
		h.log.Error("Cannot execute task: runner is nil", slog.String("task_id", taskID))
		h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":x: Task `%s` cannot be executed: runner not configured", taskID))
		return
	}

	// Get project path
	projectPath := h.getProjectPath(channelID)

	// Create execution context with cancellation
	execCtx, cancel := context.WithCancel(ctx)

	// Register as running task
	h.mu.Lock()
	key := makeConversationKey(channelID, threadTS)
	h.runningTasks[key] = &RunningTask{
		TaskID:    taskID,
		ChannelID: channelID,
		ThreadTS:  threadTS,
		StartedAt: time.Now(),
		Cancel:    cancel,
	}
	// Remove from pending
	delete(h.pendingTasks, key)
	h.mu.Unlock()

	// Clean up on completion
	defer func() {
		h.mu.Lock()
		delete(h.runningTasks, key)
		h.mu.Unlock()
	}()

	// Create the executor task
	task := &executor.Task{
		ID:          taskID,
		Title:       truncateText(description, 50),
		Description: description,
		ProjectPath: projectPath,
		Branch:      fmt.Sprintf("pilot/%s", taskID),
		CreatePR:    true,
	}

	h.log.Info("Executing task",
		slog.String("task_id", taskID),
		slog.String("channel_id", channelID),
		slog.String("project_path", projectPath))

	// Send starting message
	h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":rocket: Starting task `%s`...", taskID))

	// Set up progress callback with throttling
	var lastProgressUpdate time.Time
	const progressThrottle = 5 * time.Second
	var progressMu sync.Mutex

	progressCallback := func(tID string, phase string, progress int, message string) {
		progressMu.Lock()
		defer progressMu.Unlock()

		// Throttle progress updates to avoid flooding Slack
		if time.Since(lastProgressUpdate) < progressThrottle {
			return
		}
		lastProgressUpdate = time.Now()

		elapsed := time.Since(h.runningTasks[key].StartedAt)
		blocks := FormatProgressUpdate(phase, progress, elapsed)
		h.sendBlocksReply(ctx, channelID, threadTS, blocks, fmt.Sprintf("%s: %d%%", phase, progress))
	}

	// Register progress callback
	callbackKey := fmt.Sprintf("slack-%s", taskID)
	h.runner.AddProgressCallback(callbackKey, progressCallback)
	defer h.runner.RemoveProgressCallback(callbackKey)

	// Execute the task
	result, err := h.runner.Execute(execCtx, task)

	if err != nil {
		if execCtx.Err() == context.Canceled {
			h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":stop_sign: Task `%s` was cancelled.", taskID))
		} else {
			h.sendReply(ctx, channelID, threadTS, fmt.Sprintf(":x: Task `%s` failed: %s", taskID, err.Error()))
		}
		return
	}

	// Send result
	resultBlocks := FormatTaskResult(result)
	h.sendBlocksReply(ctx, channelID, threadTS, resultBlocks, "Task completed")

	// Record in conversation history
	resultSummary := "Task completed"
	if result.PRUrl != "" {
		resultSummary = fmt.Sprintf("Task completed with PR: %s", result.PRUrl)
	}
	h.conversationStore.Add(channelID, threadTS, "assistant", resultSummary, "")
}

// HandleCallback processes Execute/Cancel button clicks from Slack interactions
func (h *Handler) HandleCallback(ctx context.Context, action *InteractionAction) bool {
	if action == nil {
		return false
	}

	h.log.Debug("Processing callback",
		slog.String("action_id", action.ActionID),
		slog.String("value", action.Value),
		slog.String("user_id", action.UserID),
		slog.String("channel_id", action.ChannelID))

	switch action.ActionID {
	case "execute_task", "execute_plan":
		return h.handleExecuteAction(ctx, action)
	case "cancel_task", "cancel_plan":
		return h.handleCancelAction(ctx, action)
	default:
		h.log.Debug("Unknown action", slog.String("action_id", action.ActionID))
		return false
	}
}

// handleExecuteAction handles the Execute button click
func (h *Handler) handleExecuteAction(ctx context.Context, action *InteractionAction) bool {
	taskID := action.Value
	if taskID == "" {
		return false
	}

	h.mu.Lock()
	var pendingTask *PendingTask
	var foundKey string
	for key, task := range h.pendingTasks {
		if task.TaskID == taskID {
			pendingTask = task
			foundKey = key
			break
		}
	}
	h.mu.Unlock()

	if pendingTask == nil {
		h.log.Warn("Pending task not found for execute action",
			slog.String("task_id", taskID))
		// Update message to show task not found
		h.updateInteractionMessage(ctx, action, ":warning: Task not found or already executed.")
		return false
	}

	// Update the message to show execution started (remove buttons)
	h.updateInteractionMessage(ctx, action, fmt.Sprintf(":rocket: Executing task `%s`...", taskID))

	// Execute task in background
	go h.executeTask(context.Background(), pendingTask.ChannelID, pendingTask.ThreadTS, taskID, pendingTask.Description)

	// Delete from pending after we've copied the data
	h.mu.Lock()
	delete(h.pendingTasks, foundKey)
	h.mu.Unlock()

	return true
}

// handleCancelAction handles the Cancel button click
func (h *Handler) handleCancelAction(ctx context.Context, action *InteractionAction) bool {
	taskID := action.Value
	if taskID == "" {
		return false
	}

	h.mu.Lock()
	var foundKey string
	for key, task := range h.pendingTasks {
		if task.TaskID == taskID {
			foundKey = key
			break
		}
	}
	if foundKey != "" {
		delete(h.pendingTasks, foundKey)
	}
	h.mu.Unlock()

	// Update message to show cancellation
	h.updateInteractionMessage(ctx, action, fmt.Sprintf(":no_entry: Task `%s` cancelled by <@%s>", taskID, action.UserID))

	return true
}

// updateInteractionMessage updates the original interactive message
func (h *Handler) updateInteractionMessage(ctx context.Context, action *InteractionAction, text string) {
	// Build simple text blocks without buttons
	blocks := []interface{}{
		Block{
			Type: "section",
			Text: &TextObject{
				Type: "mrkdwn",
				Text: text,
			},
		},
	}

	if err := h.apiClient.UpdateInteractiveMessage(ctx, action.ChannelID, action.MessageTS, blocks, text); err != nil {
		h.log.Error("Failed to update interaction message",
			slog.String("channel_id", action.ChannelID),
			slog.Any("error", err))
	}
}

// cleanInternalSignals removes internal markers from Claude output
func cleanInternalSignals(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var cleanLines []string
	skipBlock := false

	for _, line := range lines {
		// Skip NAVIGATOR_STATUS blocks
		if strings.Contains(line, "NAVIGATOR_STATUS") {
			skipBlock = true
			continue
		}
		if skipBlock {
			// End of block when we see another separator
			if strings.HasPrefix(strings.TrimSpace(line), "━") && len(cleanLines) > 0 {
				skipBlock = false
			}
			continue
		}

		// Skip internal signals
		if strings.HasPrefix(strings.TrimSpace(line), "[SIGNAL:") {
			continue
		}
		if strings.Contains(line, "TASK_COMPLETE") || strings.Contains(line, "LOOP_CONTINUE") {
			continue
		}

		cleanLines = append(cleanLines, line)
	}

	result := strings.Join(cleanLines, "\n")
	return strings.TrimSpace(result)
}

// truncateText truncates text to maxLen characters with ellipsis
func truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

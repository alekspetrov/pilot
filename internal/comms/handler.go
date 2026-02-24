package comms

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
	"github.com/alekspetrov/pilot/internal/intent"
	"github.com/alekspetrov/pilot/internal/logging"
)

// HandlerConfig holds configuration for the shared Handler.
type HandlerConfig struct {
	Messenger      Messenger
	Runner         *executor.Runner
	Projects       ProjectSource
	ProjectPath    string
	RateLimit      *RateLimitConfig
	LLMClassifier  intent.Classifier
	ConvStore      *intent.ConversationStore
	MemberResolver MemberResolver
	Log            *slog.Logger
}

// Handler processes incoming messages with shared intent dispatch and task lifecycle.
// Both Telegram and Slack adapters delegate to this handler for core logic.
type Handler struct {
	messenger      Messenger
	runner         *executor.Runner
	projects       ProjectSource
	projectPath    string
	activeProject  map[string]string      // contextID -> projectPath
	pendingTasks   map[string]*PendingTask // contextID -> pending task
	rateLimiter    *RateLimiter
	llmClassifier  intent.Classifier
	convStore      *intent.ConversationStore
	memberResolver MemberResolver
	lastSender     map[string]string // contextID -> sender ID
	mu             sync.Mutex
	log            *slog.Logger
}

// NewHandler creates a new shared message handler.
func NewHandler(cfg *HandlerConfig) *Handler {
	logger := cfg.Log
	if logger == nil {
		logger = logging.WithComponent("comms.handler")
	}

	var rateLimiter *RateLimiter
	if cfg.RateLimit != nil {
		rateLimiter = NewRateLimiter(cfg.RateLimit)
	} else {
		rateLimiter = NewRateLimiter(DefaultRateLimitConfig())
	}

	return &Handler{
		messenger:      cfg.Messenger,
		runner:         cfg.Runner,
		projects:       cfg.Projects,
		projectPath:    cfg.ProjectPath,
		activeProject:  make(map[string]string),
		pendingTasks:   make(map[string]*PendingTask),
		rateLimiter:    rateLimiter,
		llmClassifier:  cfg.LLMClassifier,
		convStore:      cfg.ConvStore,
		memberResolver: cfg.MemberResolver,
		lastSender:     make(map[string]string),
		log:            logger,
	}
}

// HandleMessage processes an incoming message through intent dispatch.
func (h *Handler) HandleMessage(ctx context.Context, msg *IncomingMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}

	// Track sender for RBAC
	h.TrackSender(msg.ContextID, msg.SenderID)

	// Rate limiting
	if h.rateLimiter != nil && !h.rateLimiter.AllowMessage(msg.ContextID) {
		h.log.Warn("Rate limit exceeded",
			slog.String("context_id", msg.ContextID), slog.String("type", "message"))
		_ = h.messenger.SendText(ctx, msg.ContextID, msg.ThreadID,
			"⚠️ Rate limit exceeded. Please wait a moment before sending more messages.")
		return
	}

	// Check for confirmation responses
	textLower := strings.ToLower(text)
	if textLower == "yes" || textLower == "y" || textLower == "execute" || textLower == "confirm" {
		h.HandleConfirmation(ctx, msg.ContextID, msg.ThreadID, true)
		return
	}
	if textLower == "no" || textLower == "n" || textLower == "cancel" || textLower == "abort" {
		h.HandleConfirmation(ctx, msg.ContextID, msg.ThreadID, false)
		return
	}

	// Detect intent
	detectedIntent := h.detectIntent(ctx, msg.ContextID, text)
	h.log.Debug("Message received",
		slog.String("context_id", msg.ContextID),
		slog.String("intent", string(detectedIntent)))

	// Record in conversation history
	if h.convStore != nil {
		h.convStore.Add(msg.ContextID, "user", text)
	}

	switch detectedIntent {
	case intent.IntentGreeting:
		h.handleGreeting(ctx, msg.ContextID, msg.ThreadID, msg.Username)
	case intent.IntentQuestion:
		h.handleQuestion(ctx, msg.ContextID, msg.ThreadID, text)
	case intent.IntentResearch:
		h.handleResearch(ctx, msg.ContextID, msg.ThreadID, text)
	case intent.IntentPlanning:
		h.handlePlanning(ctx, msg.ContextID, msg.ThreadID, text)
	case intent.IntentChat:
		h.handleChat(ctx, msg.ContextID, msg.ThreadID, text)
	case intent.IntentTask:
		h.handleTask(ctx, msg.ContextID, msg.ThreadID, text, msg.SenderID)
	default:
		h.handleChat(ctx, msg.ContextID, msg.ThreadID, text)
	}
}

// HandleCallback processes a button click (execute/cancel).
func (h *Handler) HandleCallback(ctx context.Context, contextID, senderID, actionID string) {
	h.TrackSender(contextID, senderID)

	switch actionID {
	case "execute_task", "execute":
		h.HandleConfirmation(ctx, contextID, "", true)
	case "cancel_task", "cancel":
		h.HandleConfirmation(ctx, contextID, "", false)
	}
}

// HandleConfirmation handles task confirmation or cancellation.
func (h *Handler) HandleConfirmation(ctx context.Context, contextID, threadID string, confirmed bool) {
	h.mu.Lock()
	pending, exists := h.pendingTasks[contextID]
	if exists {
		delete(h.pendingTasks, contextID)
	}
	h.mu.Unlock()

	if !exists {
		_ = h.messenger.SendText(ctx, contextID, threadID, "No pending task to confirm.")
		return
	}

	if confirmed {
		h.executeTask(ctx, contextID, pending.ThreadID, pending.TaskID, pending.Description)
	} else {
		_ = h.messenger.SendText(ctx, contextID, pending.ThreadID,
			fmt.Sprintf("❌ Task %s cancelled.", pending.TaskID))
	}
}

// TrackSender records the sender ID for a context (for RBAC).
func (h *Handler) TrackSender(contextID, senderID string) {
	if contextID == "" || senderID == "" {
		return
	}
	h.mu.Lock()
	h.lastSender[contextID] = senderID
	h.mu.Unlock()
}

// GetLastSender returns the last sender ID for a context.
func (h *Handler) GetLastSender(contextID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastSender[contextID]
}

// GetActiveProjectPath returns the active project path for a context.
func (h *Handler) GetActiveProjectPath(contextID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if path, ok := h.activeProject[contextID]; ok {
		return path
	}
	return h.projectPath
}

// SetActiveProject sets the active project for a context by name.
func (h *Handler) SetActiveProject(contextID, projectName string) (*ProjectInfo, error) {
	if h.projects == nil {
		return nil, fmt.Errorf("no projects configured")
	}

	proj := h.projects.GetProjectByName(projectName)
	if proj == nil {
		return nil, fmt.Errorf("project '%s' not found", projectName)
	}

	h.mu.Lock()
	h.activeProject[contextID] = proj.Path
	h.mu.Unlock()

	return proj, nil
}

// GetPendingTask returns the pending task for a context, if any.
func (h *Handler) GetPendingTask(contextID string) *PendingTask {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pendingTasks[contextID]
}

// SetPendingTask sets a pending task for a context (used by adapters for custom task handling).
func (h *Handler) SetPendingTask(contextID string, task *PendingTask) {
	h.mu.Lock()
	h.pendingTasks[contextID] = task
	h.mu.Unlock()
}

// CleanupExpiredTasks removes tasks pending for more than 5 minutes.
// Call this from a periodic ticker in the adapter.
func (h *Handler) CleanupExpiredTasks(ctx context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()

	expiry := time.Now().Add(-5 * time.Minute)
	for contextID, task := range h.pendingTasks {
		if task.CreatedAt.Before(expiry) {
			_ = h.messenger.SendText(ctx, task.ContextID, task.ThreadID,
				fmt.Sprintf("⏰ Task %s expired (no confirmation received).", task.TaskID))
			delete(h.pendingTasks, contextID)
			h.log.Debug("Expired pending task",
				slog.String("task_id", task.TaskID),
				slog.String("context_id", contextID))
		}
	}
}

// detectIntent uses LLM classification with regex fallback.
func (h *Handler) detectIntent(ctx context.Context, contextID, text string) intent.Intent {
	// Fast path: commands always use regex
	if strings.HasPrefix(text, "/") {
		return intent.IntentCommand
	}

	// Fast path: clear question patterns don't need LLM
	if intent.IsClearQuestion(text) {
		return intent.IntentQuestion
	}

	// If no LLM classifier, use regex
	if h.llmClassifier == nil {
		return intent.DetectIntent(text)
	}

	// Try LLM classification with timeout
	classifyCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var history []intent.ConversationMessage
	if h.convStore != nil {
		history = h.convStore.Get(contextID)
	}

	detectedIntent, err := h.llmClassifier.Classify(classifyCtx, history, text)
	if err != nil {
		h.log.Debug("LLM classification failed, using regex", slog.Any("error", err))
		return intent.DetectIntent(text)
	}

	h.log.Debug("LLM classified intent",
		slog.String("context_id", contextID),
		slog.String("intent", string(detectedIntent)),
		slog.String("text", TruncateText(text, 50)))

	return detectedIntent
}

// resolveMemberID resolves the current sender to a team member ID.
func (h *Handler) resolveMemberID(contextID string) string {
	if h.memberResolver == nil {
		return ""
	}

	h.mu.Lock()
	senderID := h.lastSender[contextID]
	h.mu.Unlock()

	if senderID == "" {
		return ""
	}

	memberID, err := h.memberResolver.ResolveMemberID(senderID)
	if err != nil {
		h.log.Warn("failed to resolve member identity",
			slog.String("sender_id", senderID),
			slog.Any("error", err))
		return ""
	}

	if memberID != "" {
		h.log.Debug("resolved sender to team member",
			slog.String("sender_id", senderID),
			slog.String("member_id", memberID))
	}

	return memberID
}

// handleGreeting responds to greetings.
func (h *Handler) handleGreeting(ctx context.Context, contextID, threadID, username string) {
	_ = h.messenger.SendText(ctx, contextID, threadID, h.messenger.FormatGreeting(username))
}

// handleQuestion handles questions about the codebase.
func (h *Handler) handleQuestion(ctx context.Context, contextID, threadID, question string) {
	_ = h.messenger.SendText(ctx, contextID, threadID, h.messenger.FormatQuestionAck())

	questionCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Answer this question about the codebase. DO NOT make any changes, only read and analyze.

Question: %s

IMPORTANT: Be concise. Limit your exploration to 5-10 files max. Provide a brief, direct answer.
If the question is too broad, ask for clarification instead of exploring everything.`, question)

	taskID := fmt.Sprintf("Q-%d", time.Now().Unix())
	task := &executor.Task{
		ID:          taskID,
		Title:       "Question: " + TruncateText(question, 40),
		Description: prompt,
		ProjectPath: h.GetActiveProjectPath(contextID),
		Verbose:     false,
	}

	h.log.Debug("Answering question",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID))
	result, err := h.runner.Execute(questionCtx, task)

	if err != nil {
		var errMsg string
		if questionCtx.Err() == context.DeadlineExceeded {
			errMsg = "⏱ Question timed out. Try asking something more specific."
		} else {
			errMsg = "❌ Sorry, I couldn't answer that question. Try rephrasing it."
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	answer := h.messenger.CleanOutput(result.Output)
	if answer == "" {
		answer = "I couldn't find a clear answer to that question."
	}

	maxLen := h.messenger.MaxMessageLen()
	chunks := h.messenger.ChunkContent(answer, maxLen)
	for _, chunk := range chunks {
		_ = h.messenger.SendText(ctx, contextID, threadID, chunk)
		time.Sleep(200 * time.Millisecond)
	}
}

// handleResearch handles research/analysis requests.
func (h *Handler) handleResearch(ctx context.Context, contextID, threadID, query string) {
	_ = h.messenger.SendText(ctx, contextID, threadID, "🔬 Researching...")

	taskID := fmt.Sprintf("RES-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Research: " + TruncateText(query, 40),
		Description: fmt.Sprintf(`Research and analyze: %s

Provide findings in a structured format with:
- Executive summary
- Key findings
- Relevant code/files if applicable
- Recommendations

DO NOT make any code changes. This is a read-only research task.`, query),
		ProjectPath: h.GetActiveProjectPath(contextID),
		CreatePR:    false,
	}

	researchCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	h.log.Info("Executing research",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID),
		slog.String("query", TruncateText(query, 50)))
	result, err := h.runner.Execute(researchCtx, task)

	if err != nil {
		var errMsg string
		if researchCtx.Err() == context.DeadlineExceeded {
			errMsg = "⏱ Research timed out. Try a more specific query."
		} else {
			errMsg = fmt.Sprintf("❌ Research failed: %s", err.Error())
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	content := h.messenger.CleanOutput(result.Output)
	if content == "" {
		_ = h.messenger.SendText(ctx, contextID, threadID, "🤷 No research findings to report.")
		return
	}

	maxLen := h.messenger.MaxMessageLen()
	chunks := h.messenger.ChunkContent(content, maxLen)
	for i, chunk := range chunks {
		msg := chunk
		if len(chunks) > 1 {
			msg = fmt.Sprintf("📄 Part %d/%d\n\n%s", i+1, len(chunks), chunk)
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, msg)
		time.Sleep(300 * time.Millisecond)
	}
}

// handlePlanning handles planning requests.
func (h *Handler) handlePlanning(ctx context.Context, contextID, threadID, request string) {
	_ = h.messenger.SendText(ctx, contextID, threadID, "📐 Drafting plan...")

	taskID := fmt.Sprintf("PLAN-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Plan: " + TruncateText(request, 40),
		Description: fmt.Sprintf(`Create an implementation plan for: %s

Explore the codebase and propose a detailed plan. Include:
1. Summary of approach
2. Files to modify/create
3. Step-by-step implementation phases
4. Potential risks or considerations

DO NOT make any code changes. Only explore and plan.`, request),
		ProjectPath: h.GetActiveProjectPath(contextID),
		CreatePR:    false,
	}

	planTimeout := 2 * time.Minute
	if h.runner.Config() != nil && h.runner.Config().PlanningTimeout > 0 {
		planTimeout = h.runner.Config().PlanningTimeout
	}
	planCtx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()

	h.log.Info("Creating plan",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID))
	result, err := h.runner.Execute(planCtx, task)

	if err != nil {
		var errMsg string
		if planCtx.Err() == context.DeadlineExceeded {
			errMsg = "⏱ Planning timed out. Try a simpler request."
		} else {
			errMsg = fmt.Sprintf("❌ Planning failed: %s", err.Error())
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	planContent := h.messenger.CleanOutput(result.Output)
	if planContent == "" {
		var errMsg string
		switch {
		case result.Error != "":
			errMsg = fmt.Sprintf("❌ Planning error: %s", result.Error)
		case !result.Success:
			errMsg = "⏱ Planning timed out. Try a simpler request."
		default:
			errMsg = "🤷 The task may be too simple for planning. Try executing it directly."
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	// Store plan as a pending task for execution
	h.mu.Lock()
	h.pendingTasks[contextID] = &PendingTask{
		TaskID:      taskID,
		Description: fmt.Sprintf("## Implementation Plan\n\n%s\n\n## Original Request\n\n%s", planContent, request),
		ContextID:   contextID,
		ThreadID:    threadID,
		CreatedAt:   time.Now(),
	}
	h.mu.Unlock()

	// Send plan with confirmation via messenger
	summary := extractPlanSummary(planContent)
	msgRef, err := h.messenger.SendConfirmation(ctx, contextID, threadID, taskID,
		fmt.Sprintf("📋 Implementation Plan\n\n%s", summary),
		h.GetActiveProjectPath(contextID))

	if err != nil {
		// Fallback to text
		_ = h.messenger.SendText(ctx, contextID, threadID,
			fmt.Sprintf("📋 Implementation Plan\n\n%s\n\n_Reply yes to execute or no to cancel._", summary))
	} else if msgRef != "" {
		h.mu.Lock()
		if p, ok := h.pendingTasks[contextID]; ok {
			p.MessageRef = msgRef
		}
		h.mu.Unlock()
	}
}

// handleChat handles conversational messages.
func (h *Handler) handleChat(ctx context.Context, contextID, threadID, message string) {
	_ = h.messenger.SendText(ctx, contextID, threadID, "💬 Thinking...")

	taskID := fmt.Sprintf("CHAT-%d", time.Now().Unix())
	task := &executor.Task{
		ID:    taskID,
		Title: "Chat: " + TruncateText(message, 30),
		Description: fmt.Sprintf(`You are Pilot, an AI assistant for the codebase at %s.

The user wants to have a conversation (not execute a task).
Respond helpfully and conversationally. You can reference project knowledge but DO NOT make code changes.

Be concise - this is a chat conversation, not a report. Keep response under 500 words.

User message: %s`, h.GetActiveProjectPath(contextID), message),
		ProjectPath: h.GetActiveProjectPath(contextID),
		CreatePR:    false,
	}

	chatCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	h.log.Debug("Chat response",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID))
	result, err := h.runner.Execute(chatCtx, task)

	if err != nil {
		var errMsg string
		if chatCtx.Err() == context.DeadlineExceeded {
			errMsg = "⏱ Took too long to respond. Try a simpler question."
		} else {
			errMsg = "Sorry, I couldn't process that. Try rephrasing?"
		}
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	response := h.messenger.CleanOutput(result.Output)
	if response == "" {
		response = "I'm not sure how to respond to that. Could you rephrase?"
	}

	maxLen := h.messenger.MaxMessageLen()
	if len(response) > maxLen {
		response = response[:maxLen-3] + "..."
	}

	_ = h.messenger.SendText(ctx, contextID, threadID, response)

	// Record assistant response in conversation history
	if h.convStore != nil {
		h.convStore.Add(contextID, "assistant", TruncateText(response, 500))
	}
}

// handleTask handles task requests with confirmation.
func (h *Handler) handleTask(ctx context.Context, contextID, threadID, description, senderID string) {
	// Check task rate limit
	if h.rateLimiter != nil && !h.rateLimiter.AllowTask(contextID) {
		remaining := h.rateLimiter.GetRemainingTasks(contextID)
		h.log.Warn("Task rate limit exceeded",
			slog.String("context_id", contextID),
			slog.Int("remaining", remaining))
		_ = h.messenger.SendText(ctx, contextID, threadID,
			"⚠️ Task rate limit exceeded. You've submitted too many tasks recently. Please wait before submitting more.")
		return
	}

	// Check if there's already a pending task
	h.mu.Lock()
	if existing, exists := h.pendingTasks[contextID]; exists {
		h.mu.Unlock()
		_ = h.messenger.SendText(ctx, contextID, threadID,
			fmt.Sprintf("⚠️ You already have a pending task: %s\n\nReply yes to execute or no to cancel.", existing.TaskID))
		return
	}
	h.mu.Unlock()

	taskID := fmt.Sprintf("MSG-%d", time.Now().Unix())

	h.mu.Lock()
	pending := &PendingTask{
		TaskID:      taskID,
		Description: description,
		ContextID:   contextID,
		ThreadID:    threadID,
		SenderID:    senderID,
		CreatedAt:   time.Now(),
	}
	h.pendingTasks[contextID] = pending
	h.mu.Unlock()

	msgRef, err := h.messenger.SendConfirmation(ctx, contextID, threadID, taskID,
		description, h.GetActiveProjectPath(contextID))

	if err != nil {
		_ = h.messenger.SendText(ctx, contextID, threadID,
			fmt.Sprintf("📋 Task: %s\n\n%s\n\n_Reply yes to execute or no to cancel._",
				taskID, TruncateText(description, 500)))
	} else if msgRef != "" {
		h.mu.Lock()
		if p, ok := h.pendingTasks[contextID]; ok {
			p.MessageRef = msgRef
		}
		h.mu.Unlock()
	}
}

// executeTask executes a confirmed task.
func (h *Handler) executeTask(ctx context.Context, contextID, threadID, taskID, description string) {
	// Determine if ephemeral (no PR)
	createPR := true
	detectEphemeral := true
	if h.runner != nil && h.runner.Config() != nil && h.runner.Config().DetectEphemeral != nil {
		detectEphemeral = *h.runner.Config().DetectEphemeral
	}

	if detectEphemeral && intent.IsEphemeralTask(description) {
		createPR = false
		h.log.Debug("Ephemeral task detected - skipping PR creation",
			slog.String("task_id", taskID),
			slog.String("description", TruncateText(description, 50)))
	}

	// Send start message
	prNote := ""
	if !createPR {
		prNote = " (no PR)"
	}
	progressText := h.messenger.FormatProgressUpdate(taskID, "Starting"+prNote, 0, "Initializing...")
	_ = h.messenger.SendText(ctx, contextID, threadID, progressText)

	// Create task for executor
	branch := ""
	baseBranch := ""
	if createPR {
		branch = fmt.Sprintf("pilot/%s", taskID)
		baseBranch = "main"
	}

	task := &executor.Task{
		ID:          taskID,
		Title:       TruncateText(description, 50),
		Description: description,
		ProjectPath: h.GetActiveProjectPath(contextID),
		Verbose:     false,
		Branch:      branch,
		BaseBranch:  baseBranch,
		CreatePR:    createPR,
		MemberID:    h.resolveMemberID(contextID),
	}

	// Execute task
	h.log.Info("Executing task",
		slog.String("task_id", taskID),
		slog.String("context_id", contextID))
	result, err := h.runner.Execute(ctx, task)

	// Send result
	if err != nil {
		errMsg := fmt.Sprintf("❌ Task failed\n%s\n\n%s", taskID, err.Error())
		_ = h.messenger.SendText(ctx, contextID, threadID, errMsg)
		return
	}

	_ = h.messenger.SendText(ctx, contextID, threadID, h.messenger.FormatTaskResult(result))
}

// extractPlanSummary extracts key points from a plan for display.
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

		if lineCount >= 15 {
			break
		}
	}

	result := strings.Join(summary, "\n")
	if len(result) > 1500 {
		result = result[:1497] + "..."
	}

	return result
}


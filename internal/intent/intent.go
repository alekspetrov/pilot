package intent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Intent represents the detected intent of a message
type Intent string

const (
	IntentCommand  Intent = "command"
	IntentGreeting Intent = "greeting"
	IntentResearch Intent = "research"
	IntentPlanning Intent = "planning"
	IntentQuestion Intent = "question"
	IntentChat     Intent = "chat"
	IntentTask     Intent = "task"
)

// Common greeting patterns
var greetingPatterns = []string{
	"hi", "hello", "hey", "hola", "привет", "yo", "sup",
	"good morning", "good afternoon", "good evening",
	"howdy", "greetings", "what's up", "whats up",
}

// Question indicators
var questionPatterns = []string{
	"what is", "what are", "what's", "whats", "what does", "what do",
	"how do", "how does", "how can", "how to",
	"where is", "where are", "where's",
	"why is", "why are", "why does",
	"when is", "when does", "when will",
	"which", "who is", "who are",
	"can you tell", "could you explain",
	"do you know", "is there", "are there",
}

// Research patterns - indicate research/analysis requests
var researchPatterns = []string{
	"research", "analyze", "review", "investigate",
	"summarize", "compare", "evaluate", "assess",
}

// Planning patterns - indicate planning/design requests
var planningPatterns = []string{
	"plan", "design", "strategy", "how should we",
	"approach for", "architect", "outline",
}

// Chat patterns - indicate conversational/opinion requests
var chatPatterns = []string{
	"what do you think", "opinion on", "thoughts about",
	"do you recommend", "should i", "is it better",
	"discuss", "let's talk about", "lets talk about",
}

// Task action words that indicate a task request
var taskActionWords = []string{
	"create", "add", "make", "build", "implement",
	"fix", "update", "modify", "change", "edit",
	"delete", "remove", "refactor", "write",
	"generate", "setup", "configure", "install",
	// Meta-task actions (managing backlog, priorities, etc.)
	// Note: "review" moved to researchPatterns per GH-290
	"prioritize", "reprioritize", "reorder",
	"sort", "organize", "rank", "triage", "set priority",
}

// DetectIntent analyzes a message and returns the detected intent
// Priority order: Command > Greeting > Research > Planning > Question > Chat > Task
func DetectIntent(message string) Intent {
	// Normalize message
	msg := strings.ToLower(strings.TrimSpace(message))

	// 1. Commands start with /
	if strings.HasPrefix(msg, "/") {
		return IntentCommand
	}

	// 2. Check for greetings (short messages that are just greetings)
	if IsGreeting(msg) {
		return IntentGreeting
	}

	// 3. Check for research requests (research patterns with topic/URL)
	if IsResearch(msg) {
		return IntentResearch
	}

	// 4. Check for planning requests
	if IsPlanning(msg) {
		return IntentPlanning
	}

	// 5. Check for chat/conversational (opinion-seeking, no action words)
	// Checked before questions because "what do you think" starts with "what"
	if IsChat(msg) && !ContainsActionWord(msg) {
		return IntentChat
	}

	// 6. Check for questions (ends with ? or question starters)
	if IsQuestion(msg) {
		return IntentQuestion
	}

	// 7. Check for task action words
	if IsTask(msg) {
		return IntentTask
	}

	// Check for task-like references (numbers, IDs, file names)
	if ContainsTaskReference(msg) {
		return IntentTask
	}

	// Default: if message is very short AND looks like a greeting, treat as greeting
	// Otherwise treat as task (will get confirmation anyway)
	if len(msg) < 15 && IsLikelyGreeting(msg) {
		return IntentGreeting
	}

	return IntentTask
}

// ContainsTaskReference checks if message references a task, file, or specific item
func ContainsTaskReference(msg string) bool {
	// Task IDs, issue numbers, file names
	patterns := []string{
		`task[- ]?\d+`, // TASK-01, task 01
		`#\d+`,         // #123
		`\d{2,}`,       // numbers like 04, 123
		`\.\w{2,4}$`,   // file extensions
		`pick|select|open|show|do|run|work on|start`,
	}
	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, msg); matched {
			return true
		}
	}
	return false
}

// IsLikelyGreeting checks if a short message is likely just a greeting
func IsLikelyGreeting(msg string) bool {
	words := strings.Fields(msg)
	if len(words) > 3 {
		return false
	}
	for _, pattern := range greetingPatterns {
		if msg == pattern || strings.HasPrefix(msg, pattern+" ") ||
			strings.HasPrefix(msg, pattern+"!") || strings.HasPrefix(msg, pattern+",") {
			return true
		}
	}
	return false
}

// IsGreeting checks if the message is a greeting
func IsGreeting(msg string) bool {
	// Very short messages that are just greetings
	words := strings.Fields(msg)
	if len(words) <= 3 {
		for _, pattern := range greetingPatterns {
			if msg == pattern || strings.HasPrefix(msg, pattern+" ") || strings.HasPrefix(msg, pattern+"!") || strings.HasPrefix(msg, pattern+",") {
				return true
			}
		}
	}
	return false
}

// IsQuestion checks if the message is a question
func IsQuestion(msg string) bool {
	// Ends with question mark
	if strings.HasSuffix(msg, "?") {
		return true
	}

	// Starts with question patterns
	for _, pattern := range questionPatterns {
		if strings.HasPrefix(msg, pattern) {
			return true
		}
	}

	// Quick-info keywords that should be treated as questions (fast-path eligible)
	quickInfoKeywords := []string{
		"issues", "tasks", "backlog", "todos", "fixmes",
		"status", "progress", "state",
	}
	for _, keyword := range quickInfoKeywords {
		if strings.Contains(msg, keyword) && !ContainsActionWord(msg) {
			return true
		}
	}

	// Contains question-like phrases
	questionPhrases := []string{
		"tell me about", "explain", "describe",
		"show me", "list all", "find all", "list",
	}
	for _, phrase := range questionPhrases {
		if strings.Contains(msg, phrase) && !ContainsActionWord(msg) {
			return true
		}
	}

	return false
}

// IsTask checks if the message looks like a task request
func IsTask(msg string) bool {
	return ContainsActionWord(msg)
}

// IsResearch checks if the message is a research/analysis request
func IsResearch(msg string) bool {
	for _, pattern := range researchPatterns {
		// Check for pattern at start or after common prefixes
		patterns := []string{
			"^" + pattern + "\\b",           // starts with pattern
			"\\bplease " + pattern + "\\b",  // please + pattern
			"\\bcan you " + pattern + "\\b", // can you + pattern
			"\\bi need " + pattern + "\\b",  // i need + pattern
			"\\bi want " + pattern + "\\b",  // i want + pattern
		}
		for _, p := range patterns {
			if matched, _ := regexp.MatchString(p, msg); matched {
				return true
			}
		}
	}
	return false
}

// IsPlanning checks if the message is a planning/design request
func IsPlanning(msg string) bool {
	for _, pattern := range planningPatterns {
		// Use word boundary matching to avoid "architect" matching "architecture"
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(pattern) + `\b`)
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// IsChat checks if the message is conversational/opinion-seeking
func IsChat(msg string) bool {
	for _, pattern := range chatPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// ContainsActionWord checks if message contains task action words
func ContainsActionWord(msg string) bool {
	for _, action := range taskActionWords {
		// Check for action word at start or after common prefixes
		patterns := []string{
			"^" + action + "\\b",           // starts with action
			"\\bplease " + action + "\\b",  // please + action
			"\\bcan you " + action + "\\b", // can you + action
			"\\bi need " + action + "\\b",  // i need + action
			"\\bi want " + action + "\\b",  // i want + action
		}
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, msg); matched {
				return true
			}
		}
	}
	return false
}

// IsClearQuestion checks if message is unambiguously a question.
// These patterns are high-confidence and don't need LLM verification.
// Used as pre-check before LLM classification to avoid false Task classifications.
func IsClearQuestion(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))

	// Ends with question mark - very clear signal
	if strings.HasSuffix(lower, "?") {
		return true
	}

	// Clear question starters that rarely indicate tasks
	clearPatterns := []string{
		"what's in", "what is in", "whats in",
		"what's the", "what is the", "whats the",
		"how does", "how do", "how can",
		"where is", "where are", "where's",
		"why is", "why are", "why does",
		"when is", "when does", "when will",
		"who is", "who are",
		"which", "can you explain", "could you explain",
	}

	for _, pattern := range clearPatterns {
		if strings.HasPrefix(lower, pattern) {
			return true
		}
	}

	return false
}

// Description returns a human-readable description of the intent
func (i Intent) Description() string {
	switch i {
	case IntentCommand:
		return "Command"
	case IntentGreeting:
		return "Greeting"
	case IntentResearch:
		return "Research"
	case IntentPlanning:
		return "Planning"
	case IntentQuestion:
		return "Question"
	case IntentChat:
		return "Chat"
	case IntentTask:
		return "Task"
	default:
		return "Unknown"
	}
}

// Ephemeral task patterns - commands that run something but don't produce code changes
var ephemeralStartPatterns = []string{
	"serve", "run", "start", "launch", "boot",
	"npm run", "yarn", "pnpm", "cargo run", "go run", "python -m",
	"make dev", "make serve", "make run", "make start",
}

var ephemeralContainsPatterns = []string{
	"dev server", "local server", "localhost",
	"development server", "preview server",
}

var ephemeralStandalonePatterns = []string{
	"check", "test", "validate", "verify", "lint", "format",
	"build", "compile", "bundle",
}

// IsEphemeralTask checks if a task description represents an ephemeral/run command
// that shouldn't create a PR (e.g., "serve the app", "run dev server", "npm run dev")
func IsEphemeralTask(description string) bool {
	desc := strings.ToLower(strings.TrimSpace(description))

	// Early exit: if there's a modification intent, it's not ephemeral
	if ContainsModificationIntent(desc) {
		return false
	}

	// Check start patterns (commands that begin with serve/run/etc.)
	for _, pattern := range ephemeralStartPatterns {
		if strings.HasPrefix(desc, pattern) {
			return true
		}
		// Also check with common prefixes
		prefixes := []string{"please ", "can you ", "could you ", "i need to ", "i want to "}
		for _, prefix := range prefixes {
			if strings.HasPrefix(desc, prefix+pattern) {
				return true
			}
		}
	}

	// Check contains patterns (dev server, localhost, etc.)
	for _, pattern := range ephemeralContainsPatterns {
		if strings.Contains(desc, pattern) {
			return true
		}
	}

	// Check standalone patterns - only if the description is short and focused
	// (to avoid false positives like "fix the test" which should create a PR)
	words := strings.Fields(desc)
	if len(words) <= 4 {
		for _, pattern := range ephemeralStandalonePatterns {
			// Match exact word at start: "test", "check status", "lint code"
			if strings.HasPrefix(desc, pattern+" ") || desc == pattern {
				return true
			}
		}
	}

	return false
}

// ContainsModificationIntent checks if the description implies code changes
func ContainsModificationIntent(desc string) bool {
	modWords := []string{"fix", "add", "update", "change", "modify", "write", "create", "implement", "refactor"}
	for _, word := range modWords {
		if strings.Contains(desc, word) {
			return true
		}
	}
	return false
}

// ConversationMessage represents a message in the conversation
type ConversationMessage struct {
	Role    string `json:"role"` // "user" or "assistant"
	Content string `json:"content"`
}

// ConversationStore maintains recent message history per chat
type ConversationStore struct {
	mu       sync.RWMutex
	history  map[string][]ConversationMessage // chatID -> messages
	maxSize  int
	ttl      time.Duration
	lastSeen map[string]time.Time
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
func (s *ConversationStore) Add(chatID, role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.history[chatID] = append(s.history[chatID], ConversationMessage{
		Role:    role,
		Content: content,
	})

	// Trim to max size
	if len(s.history[chatID]) > s.maxSize {
		s.history[chatID] = s.history[chatID][len(s.history[chatID])-s.maxSize:]
	}

	s.lastSeen[chatID] = time.Now()
}

// Get returns the conversation history for a chat
func (s *ConversationStore) Get(chatID string) []ConversationMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if msgs, ok := s.history[chatID]; ok {
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
		for chatID, lastSeen := range s.lastSeen {
			if now.Sub(lastSeen) > s.ttl {
				delete(s.history, chatID)
				delete(s.lastSeen, chatID)
			}
		}
		s.mu.Unlock()
	}
}

// AnthropicClient provides direct access to Claude API for fast classification
type AnthropicClient struct {
	apiKey     string
	httpClient *http.Client
	model      string
}

// NewAnthropicClient creates a new Anthropic API client
func NewAnthropicClient(apiKey string) *AnthropicClient {
	return &AnthropicClient{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second, // Fast timeout for classification
		},
		model: "claude-haiku-4-5-20251001",
	}
}

// ClassifyResponse is the response from the classification API
type ClassifyResponse struct {
	Intent     string  `json:"intent"`
	Confidence float64 `json:"confidence"`
}

// Classify determines the intent of a message using Claude Haiku
func (c *AnthropicClient) Classify(ctx context.Context, messages []ConversationMessage, currentMessage string) (Intent, error) {
	// Build the classification prompt
	systemPrompt := `You are an intent classifier for a coding assistant bot. Classify the user's message into exactly one of these intents:

- command: Message starts with /
- greeting: Simple greeting like "hi", "hello", "hey"
- research: Requests for analysis/research (e.g., "research how X works", "analyze the auth flow")
- planning: Requests for implementation plans (e.g., "plan how to add X", "design a solution for Y")
- question: Questions about code/project (e.g., "what files handle auth?", "how does X work?")
- chat: Conversational/opinion-seeking (e.g., "what do you think about...", "should I...")
- task: Requests to make changes (e.g., "add a button", "fix the bug", "implement feature X")

IMPORTANT:
- "What do you think about adding X?" is CHAT (asking opinion), not TASK
- "Add X to the project" is TASK (direct instruction)
- Questions that don't require code changes are QUESTION
- Be conservative: when in doubt between task and chat, prefer chat

Respond with JSON only: {"intent": "...", "confidence": 0.0-1.0}`

	// Build messages array for API
	apiMessages := []map[string]string{}

	// Add conversation history (last 5 messages for context)
	historyStart := 0
	if len(messages) > 5 {
		historyStart = len(messages) - 5
	}
	for _, msg := range messages[historyStart:] {
		apiMessages = append(apiMessages, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}

	// Add the current message to classify
	apiMessages = append(apiMessages, map[string]string{
		"role":    "user",
		"content": fmt.Sprintf("Classify this message: %s", currentMessage),
	})

	// Build request body
	requestBody := map[string]interface{}{
		"model":      c.model,
		"max_tokens": 100,
		"system":     systemPrompt,
		"messages":   apiMessages,
		"output_config": map[string]interface{}{
			"effort": "low", // Fast classification, minimize token spend
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make API request
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	// Parse response
	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response from API")
	}

	// Parse the JSON response
	var classifyResp ClassifyResponse
	if err := json.Unmarshal([]byte(apiResp.Content[0].Text), &classifyResp); err != nil {
		return "", fmt.Errorf("failed to parse classification: %w", err)
	}

	// Map to Intent type
	switch classifyResp.Intent {
	case "command":
		return IntentCommand, nil
	case "greeting":
		return IntentGreeting, nil
	case "research":
		return IntentResearch, nil
	case "planning":
		return IntentPlanning, nil
	case "question":
		return IntentQuestion, nil
	case "chat":
		return IntentChat, nil
	case "task":
		return IntentTask, nil
	default:
		return IntentTask, nil // Default fallback
	}
}

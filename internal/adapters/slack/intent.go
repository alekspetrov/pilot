package slack

import (
	"regexp"
	"strings"
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
	"hi", "hello", "hey", "hola", "yo", "sup",
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
}

// DetectIntent analyzes a message and returns the detected intent.
// Priority order: Command > Greeting > Research > Planning > Question > Chat > Task
func DetectIntent(message string) Intent {
	// Remove Slack user mentions like <@U12345>
	msg := stripSlackMentions(message)
	msg = strings.ToLower(strings.TrimSpace(msg))

	// 1. Commands start with /
	if strings.HasPrefix(msg, "/") {
		return IntentCommand
	}

	// 2. Check for greetings (short messages that are just greetings)
	if isGreeting(msg) {
		return IntentGreeting
	}

	// 3. Check for research requests
	if isResearch(msg) {
		return IntentResearch
	}

	// 4. Check for planning requests
	if isPlanning(msg) {
		return IntentPlanning
	}

	// 5. Check for chat/conversational (opinion-seeking, no action words)
	if isChat(msg) && !containsActionWord(msg) {
		return IntentChat
	}

	// 6. Check for questions
	if isQuestion(msg) {
		return IntentQuestion
	}

	// 7. Check for task action words
	if isTask(msg) {
		return IntentTask
	}

	// Check for task-like references
	if containsTaskReference(msg) {
		return IntentTask
	}

	// Default: short messages that look like greetings
	if len(msg) < 15 && isLikelyGreeting(msg) {
		return IntentGreeting
	}

	return IntentTask
}

// stripSlackMentions removes Slack user mentions from text
func stripSlackMentions(text string) string {
	// Pattern: <@U12345> or <@U12345|username>
	re := regexp.MustCompile(`<@[UW][A-Z0-9]+(?:\|[^>]+)?>`)
	return strings.TrimSpace(re.ReplaceAllString(text, ""))
}

// isGreeting checks if the message is a greeting
func isGreeting(msg string) bool {
	words := strings.Fields(msg)
	if len(words) <= 3 {
		for _, pattern := range greetingPatterns {
			if msg == pattern ||
				strings.HasPrefix(msg, pattern+" ") ||
				strings.HasPrefix(msg, pattern+"!") ||
				strings.HasPrefix(msg, pattern+",") {
				return true
			}
		}
	}
	return false
}

// isLikelyGreeting checks if a short message is likely just a greeting
func isLikelyGreeting(msg string) bool {
	words := strings.Fields(msg)
	if len(words) > 3 {
		return false
	}
	for _, pattern := range greetingPatterns {
		if msg == pattern ||
			strings.HasPrefix(msg, pattern+" ") ||
			strings.HasPrefix(msg, pattern+"!") ||
			strings.HasPrefix(msg, pattern+",") {
			return true
		}
	}
	return false
}

// isQuestion checks if the message is a question
func isQuestion(msg string) bool {
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

	// Quick-info keywords that should be treated as questions
	quickInfoKeywords := []string{
		"issues", "tasks", "backlog", "todos", "fixmes",
		"status", "progress", "state",
	}
	for _, keyword := range quickInfoKeywords {
		if strings.Contains(msg, keyword) && !containsActionWord(msg) {
			return true
		}
	}

	// Question-like phrases
	questionPhrases := []string{
		"tell me about", "explain", "describe",
		"show me", "list all", "find all", "list",
	}
	for _, phrase := range questionPhrases {
		if strings.Contains(msg, phrase) && !containsActionWord(msg) {
			return true
		}
	}

	return false
}

// isResearch checks if the message is a research/analysis request
func isResearch(msg string) bool {
	for _, pattern := range researchPatterns {
		patterns := []string{
			"^" + pattern + "\\b",
			"\\bplease " + pattern + "\\b",
			"\\bcan you " + pattern + "\\b",
			"\\bi need " + pattern + "\\b",
			"\\bi want " + pattern + "\\b",
		}
		for _, p := range patterns {
			if matched, _ := regexp.MatchString(p, msg); matched {
				return true
			}
		}
	}
	return false
}

// isPlanning checks if the message is a planning/design request
func isPlanning(msg string) bool {
	for _, pattern := range planningPatterns {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(pattern) + `\b`)
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

// isChat checks if the message is conversational/opinion-seeking
func isChat(msg string) bool {
	for _, pattern := range chatPatterns {
		if strings.Contains(msg, pattern) {
			return true
		}
	}
	return false
}

// isTask checks if the message looks like a task request
func isTask(msg string) bool {
	return containsActionWord(msg)
}

// containsActionWord checks if message contains task action words
func containsActionWord(msg string) bool {
	for _, action := range taskActionWords {
		patterns := []string{
			"^" + action + "\\b",
			"\\bplease " + action + "\\b",
			"\\bcan you " + action + "\\b",
			"\\bi need " + action + "\\b",
			"\\bi want " + action + "\\b",
		}
		for _, pattern := range patterns {
			if matched, _ := regexp.MatchString(pattern, msg); matched {
				return true
			}
		}
	}
	return false
}

// containsTaskReference checks if message references a task, file, or specific item
func containsTaskReference(msg string) bool {
	patterns := []string{
		`task[- ]?\d+`,
		`#\d+`,
		`\d{2,}`,
		`\.\w{2,4}$`,
		`pick|select|open|show|do|run|work on|start`,
	}
	for _, pattern := range patterns {
		if matched, _ := regexp.MatchString(pattern, msg); matched {
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

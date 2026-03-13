package intent

import (
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

package intent

import (
	"testing"
)

func TestIntentDescription(t *testing.T) {
	tests := []struct {
		intent   Intent
		expected string
	}{
		{IntentCommand, "Command"},
		{IntentGreeting, "Greeting"},
		{IntentResearch, "Research"},
		{IntentPlanning, "Planning"},
		{IntentQuestion, "Question"},
		{IntentChat, "Chat"},
		{IntentTask, "Task"},
		{Intent("unknown"), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(string(tt.intent), func(t *testing.T) {
			got := tt.intent.Description()
			if got != tt.expected {
				t.Errorf("%v.Description() = %v, want %v", tt.intent, got, tt.expected)
			}
		})
	}
}

func TestIntentConstants(t *testing.T) {
	tests := []struct {
		intent   Intent
		expected string
	}{
		{IntentCommand, "command"},
		{IntentGreeting, "greeting"},
		{IntentResearch, "research"},
		{IntentPlanning, "planning"},
		{IntentQuestion, "question"},
		{IntentChat, "chat"},
		{IntentTask, "task"},
	}

	for _, tt := range tests {
		t.Run(string(tt.intent), func(t *testing.T) {
			if string(tt.intent) != tt.expected {
				t.Errorf("Intent = %q, want %q", string(tt.intent), tt.expected)
			}
		})
	}
}

func TestIsEphemeralTask(t *testing.T) {
	tests := []struct {
		name        string
		description string
		expected    bool
	}{
		// Ephemeral: serve/run commands
		{"serve the app", "serve the app", true},
		{"run dev server", "run dev server", true},
		{"start the app", "start the app", true},
		{"launch dev", "launch dev", true},
		{"boot the server", "boot the server", true},

		// Ephemeral: with polite prefixes
		{"please serve", "please serve the app", true},
		{"can you run", "can you run the server", true},
		{"could you start", "could you start dev", true},
		{"i need to run", "i need to run the app", true},
		{"i want to serve", "i want to serve locally", true},

		// Ephemeral: package manager commands
		{"npm run dev", "npm run dev", true},
		{"yarn dev", "yarn dev", true},
		{"pnpm start", "pnpm start", true},
		{"cargo run", "cargo run", true},
		{"go run main.go", "go run main.go", true},
		{"python -m flask", "python -m flask run", true},

		// Ephemeral: make commands
		{"make dev", "make dev", true},
		{"make serve", "make serve", true},
		{"make run", "make run", true},
		{"make start", "make start", true},

		// Ephemeral: dev server keywords
		{"dev server", "start the dev server", true},
		{"local server", "run local server", true},
		{"localhost", "serve on localhost", true},
		{"development server", "boot development server", true},
		{"preview server", "launch preview server", true},

		// Ephemeral: standalone check/test (short descriptions)
		{"test short", "test", true},
		{"check short", "check", true},
		{"lint short", "lint", true},
		{"build short", "build", true},
		{"format code", "format code", true},
		{"validate schema", "validate schema", true},

		// NOT ephemeral: modification tasks
		{"fix the login bug", "fix the login bug", false},
		{"add user auth", "add user authentication", false},
		{"update readme", "update the readme", false},
		{"create handler", "create a new handler", false},
		{"implement feature", "implement user logout", false},
		{"refactor auth", "refactor the auth module", false},

		// NOT ephemeral: test with modification intent
		{"fix the test", "fix the test", false},
		{"add test for auth", "add test for auth", false},
		{"update test cases", "update test cases", false},
		{"write tests", "write tests for login", false},

		// NOT ephemeral: longer descriptions even with ephemeral words
		{"run but long", "run the migration and update schema", false},
		{"check but modify", "check and fix the linting errors", false},

		// Edge cases
		{"empty string", "", false},
		{"whitespace", "   ", false},
		{"mixed case serve", "SERVE the app", true},
		{"mixed case run", "Run Dev Server", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsEphemeralTask(tt.description)
			if got != tt.expected {
				t.Errorf("IsEphemeralTask(%q) = %v, want %v", tt.description, got, tt.expected)
			}
		})
	}
}

func TestContainsModificationIntent(t *testing.T) {
	tests := []struct {
		description string
		expected    bool
	}{
		{"fix the bug", true},
		{"add new feature", true},
		{"update config", true},
		{"change settings", true},
		{"modify handler", true},
		{"write tests", true},
		{"create file", true},
		{"implement auth", true},
		{"refactor code", true},
		{"serve the app", false},
		{"run tests", false},
		{"check status", false},
		{"build project", false},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := ContainsModificationIntent(tt.description)
			if got != tt.expected {
				t.Errorf("ContainsModificationIntent(%q) = %v, want %v", tt.description, got, tt.expected)
			}
		})
	}
}

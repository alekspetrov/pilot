package slack

import "testing"

func TestDetectIntent(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected Intent
	}{
		// Greetings
		{"simple greeting hi", "hi", IntentGreeting},
		{"simple greeting hello", "hello", IntentGreeting},
		{"greeting with exclamation", "hey!", IntentGreeting},
		{"greeting with name", "hello there", IntentGreeting},
		{"greeting good morning", "good morning", IntentGreeting},

		// Questions
		{"question with mark", "what files handle auth?", IntentQuestion},
		{"what is question", "what is the database schema", IntentQuestion},
		{"how does question", "how does the API work", IntentQuestion},
		{"where is question", "where is the config file", IntentQuestion},
		{"show me request", "show me the tests", IntentQuestion},
		{"list request", "list all endpoints", IntentQuestion},

		// Research
		{"research explicit", "research how caching works", IntentResearch},
		{"analyze request", "analyze the error handling", IntentResearch},
		{"investigate request", "investigate the performance issue", IntentResearch},
		{"can you research", "can you research the architecture", IntentResearch},

		// Planning
		{"plan request", "plan how to add rate limiting", IntentPlanning},
		{"design request", "design the new API", IntentPlanning},
		{"strategy request", "strategy for migration", IntentPlanning},

		// Chat
		{"opinion request", "what do you think about redis", IntentChat},
		{"recommendation", "do you recommend using postgres", IntentChat},
		{"should i question", "should i use typescript", IntentChat},

		// Tasks
		{"add task", "add a logout button", IntentTask},
		{"create task", "create a new endpoint", IntentTask},
		{"fix task", "fix the bug in auth", IntentTask},
		{"implement task", "implement dark mode", IntentTask},
		{"update task", "update the readme", IntentTask},
		{"refactor task", "refactor the handler", IntentTask},
		{"can you add", "can you add tests", IntentTask},
		{"please create", "please create a migration", IntentTask},

		// Commands
		{"slash command", "/help", IntentCommand},
		{"status command", "/status", IntentCommand},

		// With Slack mentions
		{"mention then greeting", "<@U12345> hello", IntentGreeting},
		{"mention with username", "<@U12345|john> hi", IntentGreeting},
		{"mention then task", "<@U12345> add a button", IntentTask},
		{"mention then question", "<@U12345> what is the schema?", IntentQuestion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectIntent(tt.message)
			if got != tt.expected {
				t.Errorf("DetectIntent(%q) = %v, want %v", tt.message, got, tt.expected)
			}
		})
	}
}

func TestStripSlackMentions(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"<@U12345> hello", "hello"},
		{"<@U12345|john> hello", "hello"},
		{"<@W67890> test", "test"},
		{"hi <@U12345>", "hi"},
		{"<@U12345> <@U67890> both", "both"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := stripSlackMentions(tt.input)
			if got != tt.expected {
				t.Errorf("stripSlackMentions(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

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
				t.Errorf("Intent(%q).Description() = %q, want %q", tt.intent, got, tt.expected)
			}
		})
	}
}

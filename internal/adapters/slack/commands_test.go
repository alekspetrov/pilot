package slack

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantName string
		wantArgs string
		wantNil  bool
	}{
		{
			name:     "simple help command",
			input:    "/help",
			wantName: "help",
			wantArgs: "",
		},
		{
			name:     "status command",
			input:    "/status",
			wantName: "status",
			wantArgs: "",
		},
		{
			name:     "switch command with args",
			input:    "/switch my-project",
			wantName: "switch",
			wantArgs: "my-project",
		},
		{
			name:     "switch command with multiple word args",
			input:    "/switch my awesome project",
			wantName: "switch",
			wantArgs: "my awesome project",
		},
		{
			name:     "cancel command with task ID",
			input:    "/cancel TASK-123",
			wantName: "cancel",
			wantArgs: "TASK-123",
		},
		{
			name:     "command without leading slash",
			input:    "help",
			wantName: "help",
			wantArgs: "",
		},
		{
			name:     "command with extra whitespace",
			input:    "  /status  ",
			wantName: "status",
			wantArgs: "",
		},
		{
			name:     "mixed case command",
			input:    "/HELP",
			wantName: "help",
			wantArgs: "",
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:    "only whitespace",
			input:   "   ",
			wantNil: true,
		},
		{
			name:    "only slash",
			input:   "/",
			wantNil: true,
		},
		{
			name:     "queue command",
			input:    "/queue",
			wantName: "queue",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := ParseCommand(tt.input)

			if tt.wantNil {
				if cmd != nil {
					t.Errorf("ParseCommand(%q) = %+v, want nil", tt.input, cmd)
				}
				return
			}

			if cmd == nil {
				t.Fatalf("ParseCommand(%q) = nil, want command", tt.input)
			}

			if cmd.Name != tt.wantName {
				t.Errorf("ParseCommand(%q).Name = %q, want %q", tt.input, cmd.Name, tt.wantName)
			}

			if cmd.Args != tt.wantArgs {
				t.Errorf("ParseCommand(%q).Args = %q, want %q", tt.input, cmd.Args, tt.wantArgs)
			}
		})
	}
}

func TestIsCommand(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"/help", true},
		{"/status", true},
		{"  /switch project", true},
		{"help", false},
		{"hello /help", false},
		{"", false},
		{"/", true},
		{"   ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsCommand(tt.input)
			if got != tt.want {
				t.Errorf("IsCommand(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestKnownCommands(t *testing.T) {
	commands := KnownCommands()

	// Verify expected commands exist
	expectedCommands := []string{"help", "status", "queue", "switch", "cancel"}
	for _, expected := range expectedCommands {
		found := false
		for _, cmd := range commands {
			if cmd == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("KnownCommands() missing expected command %q", expected)
		}
	}
}

func TestFormatHelpMessage(t *testing.T) {
	msg := formatHelpMessage()

	// Verify key content is present
	if msg == "" {
		t.Error("formatHelpMessage() returned empty string")
	}

	requiredContent := []string{
		"Pilot Commands",
		"/help",
		"/status",
		"/queue",
		"/switch",
		"/cancel",
	}

	for _, content := range requiredContent {
		if !containsString(msg, content) {
			t.Errorf("formatHelpMessage() missing expected content: %q", content)
		}
	}
}

// containsString checks if s contains substr
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

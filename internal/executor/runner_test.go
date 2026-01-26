package executor

import (
	"testing"
)

func TestNewRunner(t *testing.T) {
	runner := NewRunner()

	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}
	if runner.running == nil {
		t.Error("running map not initialized")
	}
}

func TestBuildPrompt(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TASK-123",
		Title:       "Add authentication",
		Description: "Implement user authentication flow",
		ProjectPath: "/path/to/project",
		Branch:      "pilot/TASK-123",
	}

	prompt := runner.BuildPrompt(task)

	if prompt == "" {
		t.Error("buildPrompt returned empty string")
	}

	// Check that key elements are in the prompt
	tests := []string{
		"TASK-123",
		"Implement user authentication flow",
		"pilot/TASK-123",
		"Commit",
	}

	for _, expected := range tests {
		if !contains(prompt, expected) {
			t.Errorf("Prompt missing expected content: %s", expected)
		}
	}
}

func TestBuildPromptNoBranch(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "TASK-456",
		Description: "Fix a bug",
		ProjectPath: "/path/to/project",
		Branch:      "", // No branch
	}

	prompt := runner.BuildPrompt(task)

	if !contains(prompt, "current branch") {
		t.Error("Prompt should mention current branch when Branch is empty")
	}
	if contains(prompt, "Create a new git branch") {
		t.Error("Prompt should not mention creating branch when Branch is empty")
	}
}

func TestPhaseToProgress(t *testing.T) {
	runner := NewRunner()

	tests := []struct {
		phase    string
		expected int
	}{
		{"INIT", 10},
		{"RESEARCH", 10},
		{"PLAN", 25},
		{"IMPL", 50},
		{"IMPLEMENTATION", 50},
		{"VERIFY", 75},
		{"TEST", 75},
		{"COMPLETE", 100},
		{"DONE", 100},
		{"UNKNOWN", 50},
	}

	for _, tt := range tests {
		t.Run(tt.phase, func(t *testing.T) {
			result := runner.phaseToProgress(tt.phase)
			if result != tt.expected {
				t.Errorf("phaseToProgress(%s) = %d, want %d", tt.phase, result, tt.expected)
			}
		})
	}
}

func TestIsRunning(t *testing.T) {
	runner := NewRunner()

	if runner.IsRunning("nonexistent") {
		t.Error("IsRunning returned true for nonexistent task")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

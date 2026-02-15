package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBuildPromptPreCommitChecks verifies the pre-commit verification checklist (GH-1321)
func TestBuildPromptPreCommitChecks(t *testing.T) {
	// Create temp directory with .agent/ to trigger Navigator code path
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()

	task := &Task{
		ID:          "GH-1321",
		Title:       "Add pipeline hardening",
		Description: "Add external correctness checks to the pipeline to catch bugs before shipping",
		ProjectPath: tmpDir,
		Branch:      "pilot/GH-1321",
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	// GH-1321: Verify new pre-commit checklist items
	tests := []struct {
		name     string
		contains string
	}{
		{"constants sourced check", "Constants sourced"},
		{"new code tested check", "new code tested"},
		{"tests pass expanded", "If you added new exported functions or methods"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(prompt, tt.contains) {
				t.Errorf("BuildPrompt missing %q check: expected to contain %q", tt.name, tt.contains)
			}
		})
	}
}

// TestBuildPromptAcceptanceCriteriaRenumbered verifies acceptance criteria is renumbered to #6 (GH-1321)
func TestBuildPromptAcceptanceCriteriaRenumbered(t *testing.T) {
	// Create temp directory with .agent/ to trigger Navigator code path
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	runner := NewRunner()

	task := &Task{
		ID:                 "GH-1321",
		Title:              "Test task",
		Description:        "Test description with multiple features to implement including authentication and authorization",
		ProjectPath:        tmpDir,
		Branch:             "test-branch",
		AcceptanceCriteria: []string{"Criterion 1", "Criterion 2"},
	}

	prompt := runner.BuildPrompt(task, task.ProjectPath)

	// Acceptance criteria should be item #6 now
	if !strings.Contains(prompt, "6. **Acceptance criteria**") {
		t.Error("Acceptance criteria should be renumbered to item #6")
	}

	// Should NOT have old numbering
	if strings.Contains(prompt, "5. **Acceptance criteria**") {
		t.Error("Acceptance criteria should not be numbered as #5 anymore")
	}
}

// TestBuildSelfReviewPromptConstantSanityCheck verifies the constant value sanity check (GH-1321)
func TestBuildSelfReviewPromptConstantSanityCheck(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "GH-1321",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/path/to/project",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	// GH-1321: Check #6 - Constant Value Sanity
	checks := []string{
		"Constant Value Sanity Check",
		"numeric constants",
		"SUSPICIOUS_VALUE",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("Self-review prompt missing constant sanity check element: %q", check)
		}
	}
}

// TestBuildSelfReviewPromptCrossFileParity verifies the cross-file parity check (GH-1321)
func TestBuildSelfReviewPromptCrossFileParity(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "GH-1321",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/path/to/project",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	// GH-1321: Check #7 - Cross-File Parity
	checks := []string{
		"Cross-File Parity Check",
		"sibling implementations",
		"PARITY_GAP",
		"backend_*.go",
		"adapter_*.go",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("Self-review prompt missing cross-file parity check element: %q", check)
		}
	}
}

// TestBuildSelfReviewPromptCheckOrdering verifies the checks are numbered correctly (GH-1321)
func TestBuildSelfReviewPromptCheckOrdering(t *testing.T) {
	runner := NewRunner()

	task := &Task{
		ID:          "GH-1321",
		Title:       "Test task",
		Description: "Test description",
		ProjectPath: "/path/to/project",
	}

	prompt := runner.buildSelfReviewPrompt(task)

	// Verify checks are in correct order
	checks := []struct {
		num   string
		title string
	}{
		{"### 1.", "Diff Analysis"},
		{"### 2.", "Build Verification"},
		{"### 3.", "Wiring Check"},
		{"### 4.", "Method Existence Check"},
		{"### 5.", "Issue-to-Changes Alignment Check"},
		{"### 6.", "Constant Value Sanity Check"},
		{"### 7.", "Cross-File Parity Check"},
	}

	for _, check := range checks {
		expected := check.num + " " + check.title
		if !strings.Contains(prompt, expected) {
			t.Errorf("Self-review prompt missing or misnumbered check: %q", expected)
		}
	}

	// Verify Actions section comes after all checks
	check7Idx := strings.Index(prompt, "### 7.")
	actionsIdx := strings.Index(prompt, "### Actions")
	if check7Idx == -1 || actionsIdx == -1 || actionsIdx < check7Idx {
		t.Error("Actions section should come after check #7")
	}
}

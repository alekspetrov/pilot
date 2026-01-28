package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNavigatorIndexSync_HasNavigator(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Test without Navigator
	sync := NewNavigatorIndexSync(tmpDir)
	if sync.HasNavigator() {
		t.Error("HasNavigator should return false when .agent/ doesn't exist")
	}

	// Create .agent directory and DEVELOPMENT-README.md
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "DEVELOPMENT-README.md"), []byte("# Test"), 0644); err != nil {
		t.Fatalf("Failed to create index file: %v", err)
	}

	// Test with Navigator
	if !sync.HasNavigator() {
		t.Error("HasNavigator should return true when .agent/DEVELOPMENT-README.md exists")
	}
}

func TestExtractTaskNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"TASK-32", "32"},
		{"GH-57", "57"},
		{"LIN-123", "123"},
		{"task-5", "5"},
		{"gh-100", "100"},
		{"invalid", ""},
		{"TASK32", "32"},
		{"GH57", "57"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := extractTaskNumber(tt.input)
			if result != tt.expected {
				t.Errorf("extractTaskNumber(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestMatchesTask(t *testing.T) {
	tests := []struct {
		row     string
		taskID  string
		taskNum string
		want    bool
	}{
		{"| GH-57 | Some title | 🔄 In Progress |", "GH-57", "57", true},
		{"| 57 | Some title | 🔄 In Progress |", "GH-57", "57", true},
		{"| TASK-32 | Another task | ⏳ |", "TASK-32", "32", true},
		{"| 32 | Another task | ⏳ |", "TASK-32", "32", true},
		{"| 99 | Different task | 🔄 |", "GH-57", "57", false},
		{"| GH-99 | Different task | 🔄 |", "GH-57", "57", false},
	}

	for _, tt := range tests {
		t.Run(tt.taskID, func(t *testing.T) {
			result := matchesTask(tt.row, tt.taskID, tt.taskNum)
			if result != tt.want {
				t.Errorf("matchesTask(%q, %q, %q) = %v, want %v",
					tt.row, tt.taskID, tt.taskNum, result, tt.want)
			}
		})
	}
}

func TestExtractTitleFromRow(t *testing.T) {
	tests := []struct {
		row  string
		want string
	}{
		{"| GH-57 | Navigator Index Auto-Sync | 🔄 |", "Navigator Index Auto-Sync"},
		{"| 57 | Speed Optimization | 🔄 Pilot executing |", "Speed Optimization"},
		{"| TASK-32 |  Trim spaces  | ⏳ |", "Trim spaces"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			result := extractTitleFromRow(tt.row)
			if result != tt.want {
				t.Errorf("extractTitleFromRow(%q) = %q, want %q", tt.row, result, tt.want)
			}
		})
	}
}

func TestUpdateIndexContent_MovesToCompleted(t *testing.T) {
	input := `# Navigator Index

## Active Work

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |
| 54 | Speed Optimization | 🔄 In Progress |

### Backlog

Some backlog items...

---

## Completed (2026-01-28)

| Item | What |
|------|------|
| GH-52 | Full codebase audit |
`

	sync := &NavigatorIndexSync{projectPath: "/test"}
	result, changed := sync.updateIndexContent(input, "GH-57")

	if !changed {
		t.Error("Expected content to be changed")
	}

	// Should NOT contain task in In Progress
	if strings.Contains(result, "| 57 | Navigator Index") {
		t.Error("Task should be removed from In Progress section")
	}

	// Should contain task in Completed
	if !strings.Contains(result, "GH-57") || !strings.Contains(result, "Navigator Index Auto-Sync") {
		t.Error("Task should be added to Completed section")
	}

	// Other tasks should remain
	if !strings.Contains(result, "| 54 | Speed Optimization |") {
		t.Error("Other In Progress tasks should remain")
	}
}

func TestUpdateIndexContent_NoChangeIfNotFound(t *testing.T) {
	input := `# Navigator Index

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 54 | Speed Optimization | 🔄 In Progress |

## Completed

| Item | What |
|------|------|
`

	sync := &NavigatorIndexSync{projectPath: "/test"}
	_, changed := sync.updateIndexContent(input, "GH-99")

	if changed {
		t.Error("Expected no change when task not found")
	}
}

func TestUpdateIndexContent_TASK_Format(t *testing.T) {
	input := `# Navigator Index

### In Progress

| TASK# | Title | Status |
|-------|-------|--------|
| TASK-32 | Some Feature | 🔄 |

## Completed

| Item | What |
|------|------|
`

	sync := &NavigatorIndexSync{projectPath: "/test"}
	result, changed := sync.updateIndexContent(input, "TASK-32")

	if !changed {
		t.Error("Expected content to be changed for TASK format")
	}

	if strings.Contains(result, "| TASK-32 | Some Feature | 🔄 |") {
		t.Error("Task should be removed from In Progress")
	}

	if !strings.Contains(result, "TASK-32") {
		t.Error("Task should appear in Completed section")
	}
}

func TestSyncTaskCompleted_Integration(t *testing.T) {
	// Create temp directory with Navigator structure
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("Failed to create .agent dir: %v", err)
	}

	initialContent := `# Pilot Development Navigator

## Active Work

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |

---

## Completed (2026-01-28)

| Item | What |
|------|------|
| GH-52 | Codebase audit |
`

	indexPath := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	if err := os.WriteFile(indexPath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("Failed to write index file: %v", err)
	}

	// Run sync
	sync := NewNavigatorIndexSync(tmpDir)
	if err := sync.SyncTaskCompleted("GH-57"); err != nil {
		t.Fatalf("SyncTaskCompleted failed: %v", err)
	}

	// Read result
	content, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("Failed to read result: %v", err)
	}

	result := string(content)

	// Verify task moved
	if strings.Contains(result, "| 57 | Navigator Index Auto-Sync | 🔄") {
		t.Error("Task should be removed from In Progress")
	}

	// Task should be in Completed
	if !strings.Contains(result, "GH-57") {
		t.Error("Task should be in Completed section")
	}
}

func TestSyncTaskCompleted_NoNavigator(t *testing.T) {
	tmpDir := t.TempDir()

	sync := NewNavigatorIndexSync(tmpDir)
	err := sync.SyncTaskCompleted("GH-57")

	if err != nil {
		t.Errorf("SyncTaskCompleted should not error when Navigator doesn't exist: %v", err)
	}
}

func TestParseTaskFileStatus(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "TASK-32.md")

	content := `# TASK-32: Some Feature

**Status**: Completed

## Description
...
`
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write task file: %v", err)
	}

	status, err := ParseTaskFileStatus(tmpFile)
	if err != nil {
		t.Fatalf("ParseTaskFileStatus failed: %v", err)
	}

	if status != "Completed" {
		t.Errorf("Expected status 'Completed', got '%s'", status)
	}
}

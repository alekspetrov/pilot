package executor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTaskID(t *testing.T) {
	tests := []struct {
		name       string
		taskID     string
		wantNum    string
		wantPrefix string
	}{
		{
			name:       "GH format",
			taskID:     "GH-57",
			wantNum:    "57",
			wantPrefix: "GH",
		},
		{
			name:       "TASK format",
			taskID:     "TASK-123",
			wantNum:    "123",
			wantPrefix: "TASK",
		},
		{
			name:       "lowercase gh",
			taskID:     "gh-42",
			wantNum:    "42",
			wantPrefix: "GH",
		},
		{
			name:       "LIN format",
			taskID:     "LIN-789",
			wantNum:    "789",
			wantPrefix: "LIN",
		},
		{
			name:       "underscore separator",
			taskID:     "TASK_100",
			wantNum:    "100",
			wantPrefix: "TASK",
		},
		{
			name:       "invalid - no prefix",
			taskID:     "123",
			wantNum:    "",
			wantPrefix: "",
		},
		{
			name:       "invalid - no number",
			taskID:     "GH-abc",
			wantNum:    "",
			wantPrefix: "",
		},
		{
			name:       "invalid - wrong format",
			taskID:     "just-text",
			wantNum:    "",
			wantPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNum, gotPrefix := parseTaskID(tt.taskID)
			if gotNum != tt.wantNum {
				t.Errorf("parseTaskID() num = %v, want %v", gotNum, tt.wantNum)
			}
			if gotPrefix != tt.wantPrefix {
				t.Errorf("parseTaskID() prefix = %v, want %v", gotPrefix, tt.wantPrefix)
			}
		})
	}
}

func TestContainsTaskID(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		taskNum    string
		taskPrefix string
		want       bool
	}{
		{
			name:       "number in first cell",
			line:       "| 54 | Speed Optimization | 🔄 Pilot executing |",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       true,
		},
		{
			name:       "full ID in line",
			line:       "| GH-54 | Speed Optimization | 🔄 Pilot executing |",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       true,
		},
		{
			name:       "TASK format",
			line:       "| TASK-123 | Some Task | ⏳ Queued |",
			taskNum:    "123",
			taskPrefix: "TASK",
			want:       true,
		},
		{
			name:       "header row - skip",
			line:       "| GH# | Title | Status |",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       false,
		},
		{
			name:       "separator row - skip",
			line:       "|-----|-------|--------|",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       false,
		},
		{
			name:       "different task",
			line:       "| 55 | Other Task | ⏳ Queued |",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       false,
		},
		{
			name:       "non-table line",
			line:       "## Some Header",
			taskNum:    "54",
			taskPrefix: "GH",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsTaskID(tt.line, tt.taskNum, tt.taskPrefix)
			if got != tt.want {
				t.Errorf("containsTaskID() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUpdateNavigatorIndex(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		taskID      string
		taskNum     string
		taskPrefix  string
		taskTitle   string
		status      string
		wantChanged bool
		wantContain string
		wantNotContain string
	}{
		{
			name: "move GH task from In Progress to Completed",
			content: `# Navigator Index

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 54 | Speed Optimization | 🔄 Pilot executing |

---

## Completed (2026-01-28)

| Item | What |
|------|------|
| GH-52 | Previous task |
`,
			taskID:         "GH-54",
			taskNum:        "54",
			taskPrefix:     "GH",
			taskTitle:      "Speed Optimization",
			status:         "completed",
			wantChanged:    true,
			wantContain:    "GH-54",
			wantNotContain: "🔄 Pilot executing",
		},
		{
			name: "move TASK format from In Progress",
			content: `### In Progress

| TASK# | Title | Status |
|-------|-------|--------|
| TASK-100 | Implement feature | 🔄 Pilot executing |

## Completed (2026-01-28)

| Item | What |
|------|------|
`,
			taskID:         "TASK-100",
			taskNum:        "100",
			taskPrefix:     "TASK",
			taskTitle:      "Implement feature",
			status:         "completed",
			wantChanged:    true,
			wantContain:    "TASK-100",
			wantNotContain: "🔄 Pilot executing",
		},
		{
			name: "task not found - no change",
			content: `### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 55 | Other Task | 🔄 Pilot executing |

## Completed
| Item | What |
|------|------|
`,
			taskID:      "GH-54",
			taskNum:     "54",
			taskPrefix:  "GH",
			taskTitle:   "Missing Task",
			status:      "completed",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := updateNavigatorIndex(tt.content, tt.taskID, tt.taskNum, tt.taskPrefix, tt.taskTitle, tt.status)

			if changed != tt.wantChanged {
				t.Errorf("updateNavigatorIndex() changed = %v, want %v", changed, tt.wantChanged)
			}

			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Errorf("updateNavigatorIndex() result should contain %q", tt.wantContain)
			}

			if tt.wantNotContain != "" && strings.Contains(got, tt.wantNotContain) {
				t.Errorf("updateNavigatorIndex() result should not contain %q", tt.wantNotContain)
			}
		})
	}
}

func TestExtractTaskEntry(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		taskNum      string
		taskPrefix   string
		defaultTitle string
		want         string
	}{
		{
			name:         "extract from table row",
			line:         "| 54 | Speed Optimization (complexity) | 🔄 Pilot executing |",
			taskNum:      "54",
			taskPrefix:   "GH",
			defaultTitle: "Default",
			want:         "Speed Optimization (complexity)",
		},
		{
			name:         "use default when no title",
			line:         "| 54 |  | 🔄 |",
			taskNum:      "54",
			taskPrefix:   "GH",
			defaultTitle: "Default Title",
			want:         "Default Title",
		},
		{
			name:         "fallback to ID",
			line:         "| 54 |  |",
			taskNum:      "54",
			taskPrefix:   "GH",
			defaultTitle: "",
			want:         "GH-54",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTaskEntry(tt.line, tt.taskNum, tt.taskPrefix, tt.defaultTitle)
			if got != tt.want {
				t.Errorf("extractTaskEntry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStatusToEmoji(t *testing.T) {
	tests := []struct {
		status string
		want   string
	}{
		{"completed", "✅ Completed"},
		{"done", "✅ Completed"},
		{"success", "✅ Completed"},
		{"failed", "❌ Failed"},
		{"error", "❌ Failed"},
		{"in_progress", "🔄 Pilot executing"},
		{"running", "🔄 Pilot executing"},
		{"queued", "⏳ Queued"},
		{"pending", "⏳ Queued"},
		{"blocked", "🔴 Blocked"},
		{"custom", "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := statusToEmoji(tt.status)
			if got != tt.want {
				t.Errorf("statusToEmoji(%q) = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestSyncNavigatorIndex_Integration(t *testing.T) {
	// Create temp directory with Navigator structure
	tmpDir := t.TempDir()
	agentDir := filepath.Join(tmpDir, ".agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create initial index content
	initialContent := `# Development Navigator

### In Progress

| GH# | Title | Status |
|-----|-------|--------|
| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |
| 58 | Another Task | ⏳ Queued |

---

## Completed (2026-01-28)

| Item | What |
|------|------|
| GH-55 | Previous task completed |
`

	indexPath := filepath.Join(agentDir, "DEVELOPMENT-README.md")
	if err := os.WriteFile(indexPath, []byte(initialContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create runner and task
	runner := NewRunner()
	task := &Task{
		ID:          "GH-57",
		Title:       "Navigator Index Auto-Sync",
		ProjectPath: tmpDir,
	}

	// Execute sync
	if err := runner.SyncNavigatorIndex(task, "completed"); err != nil {
		t.Fatalf("SyncNavigatorIndex failed: %v", err)
	}

	// Read updated content
	updated, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	updatedStr := string(updated)

	// Verify task was removed from In Progress
	if strings.Contains(updatedStr, "| 57 | Navigator Index Auto-Sync | 🔄 Pilot executing |") {
		t.Error("Task should have been removed from In Progress section")
	}

	// Verify task was added to Completed
	if !strings.Contains(updatedStr, "GH-57") {
		t.Error("Task should appear in Completed section")
	}

	// Verify other task wasn't affected
	if !strings.Contains(updatedStr, "| 58 | Another Task | ⏳ Queued |") {
		t.Error("Other task should remain unchanged")
	}
}

func TestSyncNavigatorIndex_NoNavigator(t *testing.T) {
	// Create temp directory without Navigator structure
	tmpDir := t.TempDir()

	runner := NewRunner()
	task := &Task{
		ID:          "GH-57",
		Title:       "Test Task",
		ProjectPath: tmpDir,
	}

	// Should not error when Navigator doesn't exist
	err := runner.SyncNavigatorIndex(task, "completed")
	if err != nil {
		t.Errorf("SyncNavigatorIndex should not error for non-Navigator project: %v", err)
	}
}

func TestParseTaskRow(t *testing.T) {
	tests := []struct {
		name string
		line string
		want NavigatorTask
	}{
		{
			name: "valid task row",
			line: "| 54 | Speed Optimization | 🔄 Pilot |",
			want: NavigatorTask{ID: "54", Title: "Speed Optimization", Status: "🔄 Pilot"},
		},
		{
			name: "header row",
			line: "| GH# | Title | Status |",
			want: NavigatorTask{},
		},
		{
			name: "separator row",
			line: "|-----|-------|--------|",
			want: NavigatorTask{},
		},
		{
			name: "two column row",
			line: "| GH-52 | Completed task |",
			want: NavigatorTask{ID: "GH-52", Title: "Completed task"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTaskRow(tt.line)
			if got.ID != tt.want.ID {
				t.Errorf("parseTaskRow() ID = %v, want %v", got.ID, tt.want.ID)
			}
			if got.Title != tt.want.Title {
				t.Errorf("parseTaskRow() Title = %v, want %v", got.Title, tt.want.Title)
			}
		})
	}
}

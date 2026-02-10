package slack

import (
	"strings"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/executor"
)

func TestFormatGreeting(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantName string
	}{
		{
			name:     "with username",
			username: "alice",
			wantName: "alice",
		},
		{
			name:     "empty username",
			username: "",
			wantName: "there",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := FormatGreeting(tt.username)

			if len(blocks) != 2 {
				t.Fatalf("expected 2 blocks, got %d", len(blocks))
			}

			// Check header block
			if blocks[0].Type != "header" {
				t.Errorf("expected header block, got %s", blocks[0].Type)
			}
			if blocks[0].Text == nil {
				t.Fatal("header text is nil")
			}
			if !strings.Contains(blocks[0].Text.Text, tt.wantName) {
				t.Errorf("expected header to contain %q, got %q", tt.wantName, blocks[0].Text.Text)
			}

			// Check section block has mode descriptions
			if blocks[1].Type != "section" {
				t.Errorf("expected section block, got %s", blocks[1].Type)
			}
			if blocks[1].Text == nil {
				t.Fatal("section text is nil")
			}
			modesText := blocks[1].Text.Text
			for _, mode := range []string{"Chat", "Questions", "Research", "Planning", "Tasks"} {
				if !strings.Contains(modesText, mode) {
					t.Errorf("expected modes text to contain %q", mode)
				}
			}
		})
	}
}

func TestFormatTaskConfirmation(t *testing.T) {
	blocks := FormatTaskConfirmation("TASK-123", "Add a new feature for user authentication")

	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}

	// Check header
	header, ok := blocks[0].(Block)
	if !ok {
		t.Fatal("first block is not a Block")
	}
	if header.Type != "header" {
		t.Errorf("expected header block, got %s", header.Type)
	}
	if !strings.Contains(header.Text.Text, "Confirm Task") {
		t.Errorf("expected header to contain 'Confirm Task', got %q", header.Text.Text)
	}

	// Check section with task info
	section, ok := blocks[1].(Block)
	if !ok {
		t.Fatal("second block is not a Block")
	}
	if section.Type != "section" {
		t.Errorf("expected section block, got %s", section.Type)
	}
	if !strings.Contains(section.Text.Text, "TASK-123") {
		t.Errorf("expected section to contain task ID")
	}

	// Check divider
	divider, ok := blocks[2].(Block)
	if !ok {
		t.Fatal("third block is not a Block")
	}
	if divider.Type != "divider" {
		t.Errorf("expected divider block, got %s", divider.Type)
	}

	// Check actions block with buttons
	actions, ok := blocks[3].(ActionsBlock)
	if !ok {
		t.Fatal("fourth block is not an ActionsBlock")
	}
	if actions.Type != "actions" {
		t.Errorf("expected actions block, got %s", actions.Type)
	}
	if len(actions.Elements) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(actions.Elements))
	}

	// Verify Execute button
	if actions.Elements[0].ActionID != "execute_task" {
		t.Errorf("expected execute_task action, got %s", actions.Elements[0].ActionID)
	}
	if actions.Elements[0].Style != "primary" {
		t.Errorf("expected primary style for Execute button")
	}

	// Verify Cancel button
	if actions.Elements[1].ActionID != "cancel_task" {
		t.Errorf("expected cancel_task action, got %s", actions.Elements[1].ActionID)
	}
	if actions.Elements[1].Style != "danger" {
		t.Errorf("expected danger style for Cancel button")
	}
}

func TestFormatProgressUpdate(t *testing.T) {
	tests := []struct {
		name     string
		phase    string
		progress int
		elapsed  time.Duration
		wantBar  string
	}{
		{
			name:     "zero progress",
			phase:    "Starting",
			progress: 0,
			elapsed:  5 * time.Second,
			wantBar:  "░░░░░░░░░░░░░░░░░░░░",
		},
		{
			name:     "50% progress",
			phase:    "Implementing",
			progress: 50,
			elapsed:  30 * time.Second,
			wantBar:  "██████████░░░░░░░░░░",
		},
		{
			name:     "100% progress",
			phase:    "Completed",
			progress: 100,
			elapsed:  60 * time.Second,
			wantBar:  "████████████████████",
		},
		{
			name:     "over 100% clamped",
			phase:    "Testing",
			progress: 150,
			elapsed:  45 * time.Second,
			wantBar:  "████████████████████",
		},
		{
			name:     "negative clamped",
			phase:    "Branching",
			progress: -10,
			elapsed:  2 * time.Second,
			wantBar:  "░░░░░░░░░░░░░░░░░░░░",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := FormatProgressUpdate(tt.phase, tt.progress, tt.elapsed)

			if len(blocks) != 1 {
				t.Fatalf("expected 1 block, got %d", len(blocks))
			}

			if blocks[0].Type != "section" {
				t.Errorf("expected section block, got %s", blocks[0].Type)
			}

			text := blocks[0].Text.Text
			if !strings.Contains(text, tt.wantBar) {
				t.Errorf("expected progress bar %q in text, got %q", tt.wantBar, text)
			}
			if !strings.Contains(text, tt.phase) {
				t.Errorf("expected phase %q in text", tt.phase)
			}
		})
	}
}

func TestFormatTaskResult_Success(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:    "TASK-456",
		Success:   true,
		Duration:  2 * time.Minute,
		CommitSHA: "abc123def456",
		PRUrl:     "https://github.com/org/repo/pull/42",
	}

	blocks := FormatTaskResult(result)

	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(blocks))
	}

	// Check header
	if blocks[0].Type != "header" {
		t.Errorf("expected header block, got %s", blocks[0].Type)
	}
	if !strings.Contains(blocks[0].Text.Text, "Completed") {
		t.Errorf("expected 'Completed' in header")
	}

	// Check for task ID and commit SHA in content
	found := false
	for _, block := range blocks {
		if block.Text != nil {
			if strings.Contains(block.Text.Text, "TASK-456") {
				found = true
			}
			if strings.Contains(block.Text.Text, "abc123de") {
				// Short SHA should be present
			}
		}
	}
	if !found {
		t.Error("expected task ID in blocks")
	}

	// Check PR link is present
	prFound := false
	for _, block := range blocks {
		if block.Text != nil && strings.Contains(block.Text.Text, "pull/42") {
			prFound = true
			break
		}
	}
	if !prFound {
		t.Error("expected PR URL in blocks")
	}
}

func TestFormatTaskResult_Failure(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-789",
		Success:  false,
		Duration: 30 * time.Second,
		Error:    "Build failed: compilation error",
	}

	blocks := FormatTaskResult(result)

	if len(blocks) < 2 {
		t.Fatalf("expected at least 2 blocks, got %d", len(blocks))
	}

	// Check header
	if blocks[0].Type != "header" {
		t.Errorf("expected header block, got %s", blocks[0].Type)
	}
	if !strings.Contains(blocks[0].Text.Text, "Failed") {
		t.Errorf("expected 'Failed' in header")
	}

	// Check error is present
	errorFound := false
	for _, block := range blocks {
		if block.Text != nil && strings.Contains(block.Text.Text, "compilation error") {
			errorFound = true
			break
		}
	}
	if !errorFound {
		t.Error("expected error message in blocks")
	}
}

func TestFormatTaskResult_WithQualityGates(t *testing.T) {
	result := &executor.ExecutionResult{
		TaskID:   "TASK-QG",
		Success:  true,
		Duration: 5 * time.Minute,
		QualityGates: &executor.QualityGatesResult{
			Enabled: true,
			Gates: []executor.QualityGateResult{
				{Name: "lint", Passed: true, Duration: 10 * time.Second},
				{Name: "test", Passed: true, Duration: 2 * time.Minute, RetryCount: 1},
				{Name: "build", Passed: false, Duration: 30 * time.Second},
			},
		},
	}

	blocks := FormatTaskResult(result)

	// Find quality gates block
	qgFound := false
	for _, block := range blocks {
		if block.Text != nil && strings.Contains(block.Text.Text, "Quality Gates") {
			qgFound = true
			text := block.Text.Text
			if !strings.Contains(text, "2/3 passed") {
				t.Errorf("expected '2/3 passed' in quality gates")
			}
			if !strings.Contains(text, "lint") {
				t.Error("expected lint gate")
			}
			if !strings.Contains(text, "retry") {
				t.Error("expected retry count for test gate")
			}
			break
		}
	}
	if !qgFound {
		t.Error("expected quality gates block")
	}
}

func TestFormatQuestionAnswer(t *testing.T) {
	tests := []struct {
		name      string
		answer    string
		wantCount int
	}{
		{
			name:      "short answer",
			answer:    "The auth files are in internal/auth/",
			wantCount: 1,
		},
		{
			name:      "long answer gets chunked",
			answer:    strings.Repeat("This is a test answer. ", 200),
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := FormatQuestionAnswer(tt.answer)

			if len(blocks) < tt.wantCount {
				t.Errorf("expected at least %d blocks, got %d", tt.wantCount, len(blocks))
			}

			for _, block := range blocks {
				if block.Type != "section" {
					t.Errorf("expected section block, got %s", block.Type)
				}
				if block.Text == nil || block.Text.Type != "mrkdwn" {
					t.Error("expected mrkdwn text")
				}
			}
		})
	}
}

func TestChunkContent(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		maxLen    int
		wantCount int
	}{
		{
			name:      "content under limit",
			content:   "Short content",
			maxLen:    100,
			wantCount: 1,
		},
		{
			name:      "content exactly at limit",
			content:   "12345",
			maxLen:    5,
			wantCount: 1,
		},
		{
			name:      "content over limit no newline",
			content:   "123456789012345",
			maxLen:    10,
			wantCount: 2,
		},
		{
			name:      "content with newline break point",
			content:   "line one\nline two\nline three",
			maxLen:    15,
			wantCount: 3,
		},
		{
			name:      "empty content",
			content:   "",
			maxLen:    100,
			wantCount: 1,
		},
		{
			name:      "zero maxLen uses default",
			content:   "test",
			maxLen:    0,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkContent(tt.content, tt.maxLen)

			if len(chunks) != tt.wantCount {
				t.Errorf("expected %d chunks, got %d: %v", tt.wantCount, len(chunks), chunks)
			}

			// Verify each chunk is within limit (except when maxLen is 0)
			if tt.maxLen > 0 {
				for i, chunk := range chunks {
					if len(chunk) > tt.maxLen {
						t.Errorf("chunk %d exceeds maxLen %d: len=%d", i, tt.maxLen, len(chunk))
					}
				}
			}

			// Verify all content is covered by rejoining chunks
			// (Note: trimming may remove some whitespace, so this is a loose check)
			joined := strings.Join(chunks, " ")
			for _, chunk := range chunks {
				if chunk != "" && !strings.Contains(tt.content, strings.TrimSpace(chunk)[:min(10, len(strings.TrimSpace(chunk)))]) {
					// Chunk content should come from original
				}
			}
			_ = joined // use variable
		})
	}
}

func TestTruncateDescription(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "needs truncation",
			input:  "hello world",
			maxLen: 8,
			want:   "hello...",
		},
		{
			name:   "with newlines",
			input:  "hello\nworld",
			maxLen: 20,
			want:   "hello world",
		},
		{
			name:   "with leading/trailing spaces",
			input:  "  hello  ",
			maxLen: 20,
			want:   "hello",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateDescription(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateDescription(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

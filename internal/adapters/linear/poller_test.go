package linear

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}

	poller := NewPoller(client, config, 30*time.Second)

	if poller == nil {
		t.Fatal("NewPoller returned nil")
	}
	if poller.client != client {
		t.Error("poller.client not set correctly")
	}
	if poller.config != config {
		t.Error("poller.config not set correctly")
	}
	if poller.interval != 30*time.Second {
		t.Errorf("poller.interval = %v, want 30s", poller.interval)
	}
}

func TestWithOnLinearIssue(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}

	called := false
	callback := func(ctx context.Context, issue *Issue) (*IssueResult, error) {
		called = true
		return &IssueResult{Success: true}, nil
	}

	poller := NewPoller(client, config, 30*time.Second, WithOnLinearIssue(callback))

	if poller.onIssue == nil {
		t.Error("onIssue callback not set")
	}

	// Call the callback to verify it was set correctly
	_, _ = poller.onIssue(context.Background(), &Issue{})
	if !called {
		t.Error("callback was not called")
	}
}

func TestPoller_IsProcessed(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	// Initially should not be processed
	if poller.IsProcessed("issue-123") {
		t.Error("issue should not be processed initially")
	}

	// Mark as processed
	poller.markProcessed("issue-123")

	// Now should be processed
	if !poller.IsProcessed("issue-123") {
		t.Error("issue should be processed after marking")
	}

	// Another issue should not be processed
	if poller.IsProcessed("issue-456") {
		t.Error("other issues should not be processed")
	}
}

func TestPoller_ProcessedCount(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() = %d, want 0", poller.ProcessedCount())
	}

	poller.markProcessed("issue-1")
	if poller.ProcessedCount() != 1 {
		t.Errorf("ProcessedCount() = %d, want 1", poller.ProcessedCount())
	}

	poller.markProcessed("issue-2")
	poller.markProcessed("issue-3")
	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}

	// Re-marking same issue shouldn't increase count
	poller.markProcessed("issue-1")
	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3 after re-marking", poller.ProcessedCount())
	}
}

func TestPoller_Reset(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	// Mark some issues
	poller.markProcessed("issue-1")
	poller.markProcessed("issue-2")
	poller.markProcessed("issue-3")

	if poller.ProcessedCount() != 3 {
		t.Errorf("ProcessedCount() = %d, want 3", poller.ProcessedCount())
	}

	// Reset
	poller.Reset()

	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount() after reset = %d, want 0", poller.ProcessedCount())
	}

	if poller.IsProcessed("issue-1") {
		t.Error("issue-1 should not be processed after reset")
	}
}

func TestPoller_ConcurrentAccess(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	var wg sync.WaitGroup
	const numGoroutines = 10
	const numOpsPerGoroutine = 100

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				poller.markProcessed(string(rune('a' + base*numOpsPerGoroutine + j)))
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				_ = poller.IsProcessed(string(rune('a' + j)))
				_ = poller.ProcessedCount()
			}
		}()
	}

	wg.Wait()
}

func TestPoller_HasStatusLabel(t *testing.T) {
	client := NewClient(testutil.FakeLinearAPIKey)
	config := &WorkspaceConfig{
		Name:       "test",
		APIKey:     testutil.FakeLinearAPIKey,
		TeamID:     "TEST",
		PilotLabel: "pilot",
	}
	poller := NewPoller(client, config, 30*time.Second)

	tests := []struct {
		name     string
		labels   []Label
		expected bool
	}{
		{
			name:     "no labels",
			labels:   []Label{},
			expected: false,
		},
		{
			name:     "only pilot label",
			labels:   []Label{{Name: "pilot"}},
			expected: false,
		},
		{
			name:     "in-progress label",
			labels:   []Label{{Name: "pilot"}, {Name: LabelInProgress}},
			expected: true,
		},
		{
			name:     "done label",
			labels:   []Label{{Name: "pilot"}, {Name: LabelDone}},
			expected: true,
		},
		{
			name:     "failed label",
			labels:   []Label{{Name: "pilot"}, {Name: LabelFailed}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Labels: tt.labels}
			result := poller.hasStatusLabel(issue)
			if result != tt.expected {
				t.Errorf("hasStatusLabel() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasLabel(t *testing.T) {
	tests := []struct {
		name      string
		labels    []Label
		labelName string
		expected  bool
	}{
		{
			name:      "has label",
			labels:    []Label{{Name: "pilot"}, {Name: "bug"}},
			labelName: "pilot",
			expected:  true,
		},
		{
			name:      "does not have label",
			labels:    []Label{{Name: "pilot"}},
			labelName: "bug",
			expected:  false,
		},
		{
			name:      "empty labels",
			labels:    []Label{},
			labelName: "pilot",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &Issue{Labels: tt.labels}
			result := HasLabel(issue, tt.labelName)
			if result != tt.expected {
				t.Errorf("HasLabel() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStatusLabels(t *testing.T) {
	if LabelInProgress != "pilot-in-progress" {
		t.Errorf("LabelInProgress = %s, want 'pilot-in-progress'", LabelInProgress)
	}
	if LabelDone != "pilot-done" {
		t.Errorf("LabelDone = %s, want 'pilot-done'", LabelDone)
	}
	if LabelFailed != "pilot-failed" {
		t.Errorf("LabelFailed = %s, want 'pilot-failed'", LabelFailed)
	}
}

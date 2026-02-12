package asana

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alekspetrov/pilot/internal/testutil"
)

func TestNewPoller(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 30*time.Second)

	if poller == nil {
		t.Fatal("NewPoller returned nil")
	}
	if poller.interval != 30*time.Second {
		t.Errorf("poller.interval = %v, want 30s", poller.interval)
	}
	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount = %d, want 0", poller.ProcessedCount())
	}
}

func TestNewPoller_WithOptions(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()

	callbackCalled := false
	poller := NewPoller(client, cfg, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			callbackCalled = true
			return &TaskResult{Success: true}, nil
		}),
	)

	if poller.onTask == nil {
		t.Error("expected onTask callback to be set")
	}

	// Verify callback works
	_, _ = poller.onTask(context.Background(), &Task{})
	if !callbackCalled {
		t.Error("callback was not called")
	}
}

func TestPoller_MarkProcessed(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()

	poller := NewPoller(client, cfg, 30*time.Second)

	// Initially not processed
	if poller.IsProcessed("task-1") {
		t.Error("task should not be processed initially")
	}

	// Mark as processed
	poller.markProcessed("task-1")
	if !poller.IsProcessed("task-1") {
		t.Error("task should be processed after marking")
	}

	// Count should be 1
	if poller.ProcessedCount() != 1 {
		t.Errorf("ProcessedCount = %d, want 1", poller.ProcessedCount())
	}

	// Reset should clear
	poller.Reset()
	if poller.IsProcessed("task-1") {
		t.Error("task should not be processed after reset")
	}
	if poller.ProcessedCount() != 0 {
		t.Errorf("ProcessedCount = %d, want 0 after reset", poller.ProcessedCount())
	}
}

func TestPoller_HasStatusTag(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	poller := NewPoller(client, cfg, 30*time.Second)

	tests := []struct {
		name     string
		task     *Task
		expected bool
	}{
		{
			name:     "no tags",
			task:     &Task{Tags: []Tag{}},
			expected: false,
		},
		{
			name:     "pilot tag only",
			task:     &Task{Tags: []Tag{{Name: "pilot"}}},
			expected: false,
		},
		{
			name:     "in-progress tag",
			task:     &Task{Tags: []Tag{{Name: "pilot-in-progress"}}},
			expected: true,
		},
		{
			name:     "done tag",
			task:     &Task{Tags: []Tag{{Name: "pilot-done"}}},
			expected: true,
		},
		{
			name:     "failed tag",
			task:     &Task{Tags: []Tag{{Name: "pilot-failed"}}},
			expected: true,
		},
		{
			name:     "mixed tags with status",
			task:     &Task{Tags: []Tag{{Name: "pilot"}, {Name: "pilot-done"}, {Name: "other"}}},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := poller.hasStatusTag(tt.task)
			if got != tt.expected {
				t.Errorf("hasStatusTag() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPoller_CacheTagGIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/workspaces/"+testutil.FakeAsanaWorkspaceID+"/tags" {
			resp := PagedResponse[Tag]{
				Data: []Tag{
					{GID: "tag-pilot", Name: "pilot"},
					{GID: "tag-in-progress", Name: "pilot-in-progress"},
					{GID: "tag-done", Name: "pilot-done"},
					{GID: "tag-failed", Name: "pilot-failed"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 30*time.Second)

	err := poller.cacheTagGIDs(context.Background())
	if err != nil {
		t.Fatalf("cacheTagGIDs failed: %v", err)
	}

	if poller.pilotTagGID != "tag-pilot" {
		t.Errorf("pilotTagGID = %s, want tag-pilot", poller.pilotTagGID)
	}
	if poller.inProgressTagGID != "tag-in-progress" {
		t.Errorf("inProgressTagGID = %s, want tag-in-progress", poller.inProgressTagGID)
	}
	if poller.doneTagGID != "tag-done" {
		t.Errorf("doneTagGID = %s, want tag-done", poller.doneTagGID)
	}
	if poller.failedTagGID != "tag-failed" {
		t.Errorf("failedTagGID = %s, want tag-failed", poller.failedTagGID)
	}
}

func TestPoller_CacheTagGIDs_PilotTagNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty tag list
		resp := PagedResponse[Tag]{
			Data: []Tag{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 30*time.Second)

	err := poller.cacheTagGIDs(context.Background())
	if err == nil {
		t.Fatal("expected error when pilot tag not found")
	}
}

func TestPoller_CheckForNewTasks(t *testing.T) {
	var mu sync.Mutex
	processedTasks := []string{}
	addTagCalls := []string{}
	removeTagCalls := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/tags/tag-pilot/tasks" && r.Method == http.MethodGet:
			// Return list of tasks
			resp := PagedResponse[Task]{
				Data: []Task{
					{GID: "task-1", Name: "Task 1"},
					{GID: "task-2", Name: "Task 2"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/tasks/task-1" && r.Method == http.MethodGet:
			// Return full task details
			resp := APIResponse[Task]{
				Data: Task{
					GID:       "task-1",
					Name:      "Task 1",
					Notes:     "Description 1",
					Completed: false,
					Tags:      []Tag{{GID: "tag-pilot", Name: "pilot"}},
					CreatedAt: time.Now().Add(-time.Hour),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/tasks/task-2" && r.Method == http.MethodGet:
			// Already has status tag
			resp := APIResponse[Task]{
				Data: Task{
					GID:       "task-2",
					Name:      "Task 2",
					Completed: false,
					Tags:      []Tag{{GID: "tag-pilot", Name: "pilot"}, {GID: "tag-done", Name: "pilot-done"}},
					CreatedAt: time.Now(),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/tasks/task-1/addTag" && r.Method == http.MethodPost:
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := body["data"].(map[string]interface{})
			addTagCalls = append(addTagCalls, data["tag"].(string))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))

		case r.URL.Path == "/tasks/task-1/removeTag" && r.Method == http.MethodPost:
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := body["data"].(map[string]interface{})
			removeTagCalls = append(removeTagCalls, data["tag"].(string))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))

		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			mu.Lock()
			processedTasks = append(processedTasks, task.GID)
			mu.Unlock()
			return &TaskResult{Success: true, PRNumber: 123, PRURL: "https://github.com/test/pr/123"}, nil
		}),
	)

	// Set up tag GIDs manually (normally done in Start)
	poller.pilotTagGID = "tag-pilot"
	poller.inProgressTagGID = "tag-in-progress"
	poller.doneTagGID = "tag-done"
	poller.failedTagGID = "tag-failed"

	// Run check
	poller.checkForNewTasks(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// Should have processed only task-1 (task-2 has pilot-done tag)
	if len(processedTasks) != 1 {
		t.Errorf("processed %d tasks, want 1", len(processedTasks))
	}
	if len(processedTasks) > 0 && processedTasks[0] != "task-1" {
		t.Errorf("processed task %s, want task-1", processedTasks[0])
	}

	// Should have added in-progress tag before processing
	if len(addTagCalls) < 1 || addTagCalls[0] != "tag-in-progress" {
		t.Errorf("expected in-progress tag to be added first, got %v", addTagCalls)
	}

	// Should have removed in-progress and added done tag after success
	if len(removeTagCalls) < 1 || removeTagCalls[0] != "tag-in-progress" {
		t.Errorf("expected in-progress tag to be removed, got %v", removeTagCalls)
	}
}

func TestPoller_CheckForNewTasks_HandlerError(t *testing.T) {
	var mu sync.Mutex
	addTagCalls := []string{}
	removeTagCalls := []string{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.URL.Path == "/tags/tag-pilot/tasks":
			resp := PagedResponse[Task]{
				Data: []Task{{GID: "task-1", Name: "Task 1"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/tasks/task-1" && r.Method == http.MethodGet:
			resp := APIResponse[Task]{
				Data: Task{
					GID:       "task-1",
					Name:      "Task 1",
					Completed: false,
					Tags:      []Tag{{GID: "tag-pilot", Name: "pilot"}},
					CreatedAt: time.Now(),
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)

		case r.URL.Path == "/tasks/task-1/addTag":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := body["data"].(map[string]interface{})
			addTagCalls = append(addTagCalls, data["tag"].(string))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))

		case r.URL.Path == "/tasks/task-1/removeTag":
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			data := body["data"].(map[string]interface{})
			removeTagCalls = append(removeTagCalls, data["tag"].(string))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))

		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 30*time.Second,
		WithOnAsanaTask(func(ctx context.Context, task *Task) (*TaskResult, error) {
			return nil, context.Canceled // Simulate error
		}),
	)

	poller.pilotTagGID = "tag-pilot"
	poller.inProgressTagGID = "tag-in-progress"
	poller.doneTagGID = "tag-done"
	poller.failedTagGID = "tag-failed"

	poller.checkForNewTasks(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// On error: should remove in-progress and add failed
	foundInProgressRemove := false
	for _, call := range removeTagCalls {
		if call == "tag-in-progress" {
			foundInProgressRemove = true
		}
	}
	if !foundInProgressRemove {
		t.Error("expected in-progress tag to be removed on error")
	}

	foundFailedAdd := false
	for _, call := range addTagCalls {
		if call == "tag-failed" {
			foundFailedAdd = true
		}
	}
	if !foundFailedAdd {
		t.Error("expected failed tag to be added on error")
	}

	// Task should be marked as processed even on error
	if !poller.IsProcessed("task-1") {
		t.Error("task should be marked as processed even on error")
	}
}

func TestPoller_Start_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/workspaces/"+testutil.FakeAsanaWorkspaceID+"/tags" {
			resp := PagedResponse[Tag]{
				Data: []Tag{{GID: "tag-pilot", Name: "pilot"}},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		if r.URL.Path == "/tags/tag-pilot/tasks" {
			resp := PagedResponse[Task]{Data: []Task{}}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClientWithBaseURL(server.URL, testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	cfg.PilotTag = "pilot"

	poller := NewPoller(client, cfg, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- poller.Start(ctx)
	}()

	// Let it run one cycle
	time.Sleep(150 * time.Millisecond)

	// Cancel context
	cancel()

	// Should stop gracefully
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Start did not stop after context cancel")
	}
}

func TestPoller_ConcurrentAccess(t *testing.T) {
	client := NewClient(testutil.FakeAsanaAccessToken, testutil.FakeAsanaWorkspaceID)
	cfg := DefaultConfig()
	poller := NewPoller(client, cfg, 30*time.Second)

	// Test concurrent read/write access
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		taskID := string(rune('a' + i%26))

		go func(id string) {
			defer wg.Done()
			poller.markProcessed(id)
		}(taskID)

		go func(id string) {
			defer wg.Done()
			_ = poller.IsProcessed(id)
		}(taskID)
	}
	wg.Wait()

	// Should not panic and count should be reasonable
	count := poller.ProcessedCount()
	if count < 1 || count > 26 {
		t.Errorf("unexpected processed count: %d", count)
	}
}

func TestTaskResult(t *testing.T) {
	result := &TaskResult{
		Success:  true,
		PRNumber: 123,
		PRURL:    "https://github.com/test/repo/pull/123",
		Error:    nil,
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.PRNumber != 123 {
		t.Errorf("PRNumber = %d, want 123", result.PRNumber)
	}
	if result.PRURL != "https://github.com/test/repo/pull/123" {
		t.Errorf("PRURL = %s, want https://github.com/test/repo/pull/123", result.PRURL)
	}
}

func TestPollingConfig(t *testing.T) {
	cfg := &PollingConfig{
		Enabled:    true,
		Interval:   60 * time.Second,
		ProjectGID: "proj-123",
	}

	if !cfg.Enabled {
		t.Error("expected Enabled to be true")
	}
	if cfg.Interval != 60*time.Second {
		t.Errorf("Interval = %v, want 60s", cfg.Interval)
	}
	if cfg.ProjectGID != "proj-123" {
		t.Errorf("ProjectGID = %s, want proj-123", cfg.ProjectGID)
	}
}

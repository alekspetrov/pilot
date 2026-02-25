package plane

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

// Test UUIDs for labels and work items
const (
	testPilotLabelID      = "label-pilot-uuid"
	testInProgressLabelID = "label-in-progress-uuid"
	testDoneLabelID       = "label-done-uuid"
	testFailedLabelID     = "label-failed-uuid"
	testProjectID         = "project-uuid-1"
	testWorkspaceSlug     = "test-workspace"
)

func TestNewPoller(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.interval != 30*time.Second {
		t.Errorf("expected interval 30s, got %v", poller.interval)
	}

	if len(poller.processed) != 0 {
		t.Error("expected empty processed map")
	}
}

func TestPollerWithOptions(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	var callbackCalled bool
	handler := func(ctx context.Context, issue *WorkItem) (*IssueResult, error) {
		callbackCalled = true
		return &IssueResult{Success: true}, nil
	}

	poller := NewPoller(client, config, 30*time.Second,
		WithOnIssue(handler),
	)

	if poller.onIssue == nil {
		t.Error("expected onIssue handler to be set")
	}

	// Call the handler to verify it's wired correctly
	_, _ = poller.onIssue(context.Background(), &WorkItem{})
	if !callbackCalled {
		t.Error("expected callback to be called")
	}
}

func TestPollerWithOnPRCreated(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	var prCalled bool
	poller := NewPoller(client, config, 30*time.Second,
		WithOnPRCreated(func(prNumber int, prURL, issueID, headSHA, branchName string) {
			prCalled = true
		}),
	)

	if poller.onPRCreated == nil {
		t.Error("expected onPRCreated handler to be set")
	}

	poller.onPRCreated(42, "https://github.com/org/repo/pull/42", "issue-uuid", "abc123", "pilot/PLANE-1")
	if !prCalled {
		t.Error("expected PR callback to be called")
	}
}

func TestPollerMarkProcessed(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}
	poller := NewPoller(client, config, 30*time.Second)

	if poller.IsProcessed("uuid-1") {
		t.Error("expected uuid-1 NOT to be processed initially")
	}

	poller.markProcessed("uuid-1")

	if !poller.IsProcessed("uuid-1") {
		t.Error("expected uuid-1 to be processed after marking")
	}

	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1, got %d", poller.ProcessedCount())
	}

	poller.Reset()

	if poller.IsProcessed("uuid-1") {
		t.Error("expected uuid-1 NOT to be processed after reset")
	}

	if poller.ProcessedCount() != 0 {
		t.Errorf("expected processed count 0 after reset, got %d", poller.ProcessedCount())
	}
}

func TestPollerClearProcessed(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}
	poller := NewPoller(client, config, 30*time.Second)

	poller.markProcessed("uuid-1")
	poller.markProcessed("uuid-2")

	if poller.ProcessedCount() != 2 {
		t.Errorf("expected processed count 2, got %d", poller.ProcessedCount())
	}

	poller.ClearProcessed("uuid-1")

	if poller.IsProcessed("uuid-1") {
		t.Error("expected uuid-1 NOT to be processed after clearing")
	}
	if !poller.IsProcessed("uuid-2") {
		t.Error("expected uuid-2 to still be processed")
	}
	if poller.ProcessedCount() != 1 {
		t.Errorf("expected processed count 1 after clearing one, got %d", poller.ProcessedCount())
	}
}

func TestPollerConcurrentAccess(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}
	poller := NewPoller(client, config, 30*time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			id := "uuid-" + string(rune('0'+n%10))
			poller.markProcessed(id)
			_ = poller.IsProcessed(id)
			_ = poller.ProcessedCount()
		}(i)
	}
	wg.Wait()

	count := poller.ProcessedCount()
	if count == 0 {
		t.Error("expected some processed items")
	}
}

func TestPollerHasStatusLabel(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}
	poller := NewPoller(client, config, 30*time.Second)

	// Set cached label IDs
	poller.pilotLabelID = testPilotLabelID
	poller.inProgressLabelID = testInProgressLabelID
	poller.doneLabelID = testDoneLabelID
	poller.failedLabelID = testFailedLabelID

	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"no labels", []string{}, false},
		{"pilot only", []string{testPilotLabelID}, false},
		{"in-progress", []string{testPilotLabelID, testInProgressLabelID}, true},
		{"done", []string{testPilotLabelID, testDoneLabelID}, true},
		{"failed", []string{testPilotLabelID, testFailedLabelID}, true},
		{"unrelated labels", []string{"some-other-uuid"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &WorkItem{Labels: tt.labels}
			got := poller.hasStatusLabel(item)
			if got != tt.want {
				t.Errorf("hasStatusLabel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasLabelID(t *testing.T) {
	tests := []struct {
		name    string
		labels  []string
		labelID string
		want    bool
	}{
		{"exact match", []string{testPilotLabelID}, testPilotLabelID, true},
		{"not found", []string{testPilotLabelID}, "other-uuid", false},
		{"empty labels", []string{}, testPilotLabelID, false},
		{"multiple labels", []string{"a", "b", testDoneLabelID}, testDoneLabelID, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &WorkItem{Labels: tt.labels}
			got := HasLabelID(item, tt.labelID)
			if got != tt.want {
				t.Errorf("HasLabelID() = %v, want %v", got, tt.want)
			}
		})
	}
}

// newTestServer creates an httptest.Server that simulates the Plane API for poller tests.
func newTestServer(t *testing.T, labels []Label, workItems []WorkItem) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Labels endpoint
		if r.Method == http.MethodGet && contains(r.URL.Path, "/labels/") {
			resp := ListResponse[Label]{Results: labels, TotalCount: len(labels)}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Work items list endpoint (GET with query param)
		if r.Method == http.MethodGet && contains(r.URL.Path, "/work-items/") && !hasWorkItemID(r.URL.Path) {
			// Filter by label if provided
			labelFilter := r.URL.Query().Get("label")
			var filtered []WorkItem
			for _, item := range workItems {
				if labelFilter == "" || hasLabel(item.Labels, labelFilter) {
					filtered = append(filtered, item)
				}
			}
			resp := ListResponse[WorkItem]{Results: filtered, TotalCount: len(filtered)}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		// Single work item GET (for AddLabel/RemoveLabel)
		if r.Method == http.MethodGet && contains(r.URL.Path, "/work-items/") {
			for _, item := range workItems {
				if contains(r.URL.Path, item.ID) {
					_ = json.NewEncoder(w).Encode(item)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}

		// Work item PATCH (label update)
		if r.Method == http.MethodPatch && contains(r.URL.Path, "/work-items/") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasWorkItemID(path string) bool {
	// Check if path ends with a UUID-like segment (not just /work-items/)
	parts := splitPath(path)
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	// Work item IDs are UUIDs, list endpoint ends with "work-items"
	return last != "work-items" && last != ""
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func hasLabel(labels []string, labelID string) bool {
	for _, l := range labels {
		if l == labelID {
			return true
		}
	}
	return false
}

func TestPollerCacheLabelIDs(t *testing.T) {
	labels := []Label{
		{ID: testPilotLabelID, Name: "pilot"},
		{ID: testInProgressLabelID, Name: "pilot-in-progress"},
		{ID: testDoneLabelID, Name: "pilot-done"},
		{ID: testFailedLabelID, Name: "pilot-failed"},
	}

	server := newTestServer(t, labels, nil)
	defer server.Close()

	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 30*time.Second)

	err := poller.cacheLabelIDs(context.Background())
	if err != nil {
		t.Fatalf("cacheLabelIDs() error = %v", err)
	}

	if poller.pilotLabelID != testPilotLabelID {
		t.Errorf("pilotLabelID = %q, want %q", poller.pilotLabelID, testPilotLabelID)
	}
	if poller.inProgressLabelID != testInProgressLabelID {
		t.Errorf("inProgressLabelID = %q, want %q", poller.inProgressLabelID, testInProgressLabelID)
	}
	if poller.doneLabelID != testDoneLabelID {
		t.Errorf("doneLabelID = %q, want %q", poller.doneLabelID, testDoneLabelID)
	}
	if poller.failedLabelID != testFailedLabelID {
		t.Errorf("failedLabelID = %q, want %q", poller.failedLabelID, testFailedLabelID)
	}
}

func TestPollerCacheLabelIDs_NotFound(t *testing.T) {
	// No labels at all
	server := newTestServer(t, nil, nil)
	defer server.Close()

	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 30*time.Second)

	err := poller.cacheLabelIDs(context.Background())
	if err == nil {
		t.Error("expected error when pilot label not found")
	}
}

func TestPollerCheckForNewIssues(t *testing.T) {
	labels := []Label{
		{ID: testPilotLabelID, Name: "pilot"},
		{ID: testInProgressLabelID, Name: "pilot-in-progress"},
		{ID: testDoneLabelID, Name: "pilot-done"},
		{ID: testFailedLabelID, Name: "pilot-failed"},
	}

	workItems := []WorkItem{
		{
			ID:        "item-1",
			Name:      "First issue",
			Labels:    []string{testPilotLabelID},
			ProjectID: testProjectID,
			CreatedAt: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "item-2",
			Name:      "Second issue (in progress)",
			Labels:    []string{testPilotLabelID, testInProgressLabelID},
			ProjectID: testProjectID,
			CreatedAt: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
		},
	}

	server := newTestServer(t, labels, workItems)
	defer server.Close()

	var processedItem *WorkItem
	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *WorkItem) (*IssueResult, error) {
			processedItem = issue
			return &IssueResult{Success: true}, nil
		}),
	)

	// Set cached label IDs (bypass cacheLabelIDs for unit test)
	poller.pilotLabelID = testPilotLabelID
	poller.inProgressLabelID = testInProgressLabelID
	poller.doneLabelID = testDoneLabelID
	poller.failedLabelID = testFailedLabelID

	ctx := context.Background()
	poller.checkForNewIssues(ctx)
	poller.WaitForActive()

	// Should process item-1 but skip item-2 (has in-progress label)
	if processedItem == nil {
		t.Fatal("expected a work item to be processed")
	}

	if processedItem.ID != "item-1" {
		t.Errorf("expected item-1 to be processed, got %s", processedItem.ID)
	}

	if !poller.IsProcessed("item-1") {
		t.Error("expected item-1 to be marked as processed")
	}
}

func TestPollerCheckForNewIssues_SkipsAlreadyProcessed(t *testing.T) {
	labels := []Label{
		{ID: testPilotLabelID, Name: "pilot"},
	}
	workItems := []WorkItem{
		{
			ID:        "item-1",
			Name:      "Already processed",
			Labels:    []string{testPilotLabelID},
			ProjectID: testProjectID,
		},
	}

	server := newTestServer(t, labels, workItems)
	defer server.Close()

	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}

	var callCount int
	poller := NewPoller(client, config, 30*time.Second,
		WithOnIssue(func(ctx context.Context, issue *WorkItem) (*IssueResult, error) {
			callCount++
			return &IssueResult{Success: true}, nil
		}),
	)

	poller.pilotLabelID = testPilotLabelID

	// Mark as already processed
	poller.markProcessed("item-1")

	ctx := context.Background()
	poller.checkForNewIssues(ctx)

	if callCount != 0 {
		t.Errorf("expected callback not to be called for already processed issue, got %d calls", callCount)
	}
}

func TestPollerStart_CancelsOnContextDone(t *testing.T) {
	labels := []Label{
		{ID: testPilotLabelID, Name: "pilot"},
	}

	server := newTestServer(t, labels, nil)
	defer server.Close()

	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 100*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- poller.Start(ctx)
	}()

	// Cancel after a short delay
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected nil error on cancel, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("poller did not stop after context cancellation")
	}
}

// mockProcessedStore implements ProcessedStore for testing.
type mockProcessedStore struct {
	mu        sync.Mutex
	processed map[string]string // id -> result
}

func newMockProcessedStore() *mockProcessedStore {
	return &mockProcessedStore{
		processed: make(map[string]string),
	}
}

func (m *mockProcessedStore) MarkPlaneIssueProcessed(issueID string, result string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed[issueID] = result
	return nil
}

func (m *mockProcessedStore) UnmarkPlaneIssueProcessed(issueID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.processed, issueID)
	return nil
}

func (m *mockProcessedStore) IsPlaneIssueProcessed(issueID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.processed[issueID]
	return ok, nil
}

func (m *mockProcessedStore) LoadPlaneProcessedIssues() (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]bool)
	for key := range m.processed {
		result[key] = true
	}
	return result, nil
}

func TestPoller_LoadsProcessedFromStore(t *testing.T) {
	store := newMockProcessedStore()

	// Pre-populate store with processed issues
	store.processed["uuid-1"] = "processed"
	store.processed["uuid-2"] = "processed"

	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	if poller.ProcessedCount() != 2 {
		t.Errorf("ProcessedCount = %d, want 2", poller.ProcessedCount())
	}

	if !poller.IsProcessed("uuid-1") {
		t.Error("uuid-1 should be processed")
	}
	if !poller.IsProcessed("uuid-2") {
		t.Error("uuid-2 should be processed")
	}
	if poller.IsProcessed("uuid-3") {
		t.Error("uuid-3 should not be processed")
	}
}

func TestPoller_MarkProcessed_PersistsToStore(t *testing.T) {
	store := newMockProcessedStore()
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	poller.markProcessed("uuid-new")

	if !poller.IsProcessed("uuid-new") {
		t.Error("uuid-new should be processed in memory")
	}

	store.mu.Lock()
	_, exists := store.processed["uuid-new"]
	store.mu.Unlock()
	if !exists {
		t.Error("uuid-new should be persisted to store")
	}
}

func TestPoller_ClearProcessed_RemovesFromStore(t *testing.T) {
	store := newMockProcessedStore()
	store.processed["uuid-clear"] = "processed"

	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second, WithProcessedStore(store))

	if !poller.IsProcessed("uuid-clear") {
		t.Error("uuid-clear should be processed initially")
	}

	poller.ClearProcessed("uuid-clear")

	if poller.IsProcessed("uuid-clear") {
		t.Error("uuid-clear should not be processed after clearing")
	}

	store.mu.Lock()
	_, exists := store.processed["uuid-clear"]
	store.mu.Unlock()
	if exists {
		t.Error("uuid-clear should be removed from store")
	}
}

func TestPoller_WithMaxConcurrent(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(5))

	if poller.maxConcurrent != 5 {
		t.Errorf("maxConcurrent = %d, want 5", poller.maxConcurrent)
	}

	if cap(poller.semaphore) != 5 {
		t.Errorf("semaphore capacity = %d, want 5", cap(poller.semaphore))
	}
}

func TestPoller_WithMaxConcurrent_DefaultsToTwo(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second)

	if poller.maxConcurrent != 2 {
		t.Errorf("default maxConcurrent = %d, want 2", poller.maxConcurrent)
	}
}

func TestPoller_WithMaxConcurrent_MinimumOne(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(0))

	if poller.maxConcurrent != 1 {
		t.Errorf("maxConcurrent with 0 = %d, want 1 (minimum)", poller.maxConcurrent)
	}

	poller2 := NewPoller(client, config, 30*time.Second, WithMaxConcurrent(-5))

	if poller2.maxConcurrent != 1 {
		t.Errorf("maxConcurrent with -5 = %d, want 1 (minimum)", poller2.maxConcurrent)
	}
}

func TestPoller_Drain(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second)

	// Simulate an active task
	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	drainDone := make(chan struct{})
	go func() {
		poller.Drain()
		close(drainDone)
	}()

	// Give Drain time to set stopping flag
	time.Sleep(10 * time.Millisecond)

	if !poller.stopping.Load() {
		t.Error("stopping should be true after Drain called")
	}

	// Complete the active task
	<-poller.semaphore
	poller.activeWg.Done()

	select {
	case <-drainDone:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("Drain should complete after active tasks finish")
	}
}

func TestPoller_WaitForActive(t *testing.T) {
	client := NewClient("https://api.plane.so", testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{PilotLabel: "pilot", ProjectIDs: []string{testProjectID}}

	poller := NewPoller(client, config, 30*time.Second)

	// Simulate an active task
	poller.semaphore <- struct{}{}
	poller.activeWg.Add(1)

	waitDone := make(chan struct{})
	go func() {
		poller.WaitForActive()
		close(waitDone)
	}()

	// Give WaitForActive time to set stopping flag
	time.Sleep(10 * time.Millisecond)

	if !poller.stopping.Load() {
		t.Error("stopping should be true after WaitForActive called")
	}

	// Complete the active task
	<-poller.semaphore
	poller.activeWg.Done()

	select {
	case <-waitDone:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Error("WaitForActive should complete after active tasks finish")
	}
}

func TestPollerRecoverOrphanedIssues(t *testing.T) {
	labels := []Label{
		{ID: testPilotLabelID, Name: "pilot"},
		{ID: testInProgressLabelID, Name: "pilot-in-progress"},
	}

	orphanedItems := []WorkItem{
		{
			ID:        "orphan-1",
			Name:      "Orphaned issue",
			Labels:    []string{testPilotLabelID, testInProgressLabelID},
			ProjectID: testProjectID,
		},
	}

	var patchCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodGet && containsStr(r.URL.Path, "/labels/") {
			resp := ListResponse[Label]{Results: labels, TotalCount: len(labels)}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodGet && containsStr(r.URL.Path, "/work-items/") && !containsStr(r.URL.Path, "orphan-1") {
			// List with in-progress label filter
			resp := ListResponse[WorkItem]{Results: orphanedItems, TotalCount: 1}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		if r.Method == http.MethodGet && containsStr(r.URL.Path, "orphan-1") {
			_ = json.NewEncoder(w).Encode(orphanedItems[0])
			return
		}

		if r.Method == http.MethodPatch {
			patchCount++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, testutil.FakePlaneAPIKey, testWorkspaceSlug)
	config := &Config{
		PilotLabel:    "pilot",
		WorkspaceSlug: testWorkspaceSlug,
		ProjectIDs:    []string{testProjectID},
	}
	poller := NewPoller(client, config, 30*time.Second)
	poller.inProgressLabelID = testInProgressLabelID

	poller.recoverOrphanedIssues(context.Background())

	// Should have made a PATCH call to remove the in-progress label
	if patchCount == 0 {
		t.Error("expected at least one PATCH call to remove in-progress label")
	}
}

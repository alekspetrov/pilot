package memory

import (
	"os"
	"testing"
	"time"
)

func TestNewGlobalPatternStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	if store == nil {
		t.Error("NewGlobalPatternStore() returned nil")
	}

	if store.Count() != 0 {
		t.Errorf("new store Count() = %d, want 0", store.Count())
	}
}

func TestGlobalPatternStore_AddAndGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	pattern := &GlobalPattern{
		ID:          "test-pattern-1",
		Type:        PatternTypeCode,
		Title:       "Test Pattern",
		Description: "A test pattern",
		Confidence:  0.9,
	}

	if err := store.Add(pattern); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, ok := store.Get("test-pattern-1")
	if !ok {
		t.Fatal("Get() returned false, want true")
	}

	if got.Title != "Test Pattern" {
		t.Errorf("Get().Title = %q, want %q", got.Title, "Test Pattern")
	}

	if got.Uses != 1 {
		t.Errorf("Get().Uses = %d, want 1", got.Uses)
	}
}

func TestGlobalPatternStore_GetByType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	patterns := []*GlobalPattern{
		{ID: "code-1", Type: PatternTypeCode, Title: "Code 1"},
		{ID: "code-2", Type: PatternTypeCode, Title: "Code 2"},
		{ID: "workflow-1", Type: PatternTypeWorkflow, Title: "Workflow 1"},
	}

	for _, p := range patterns {
		if err := store.Add(p); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	codePatterns := store.GetByType(PatternTypeCode)
	if len(codePatterns) != 2 {
		t.Errorf("GetByType(Code) returned %d patterns, want 2", len(codePatterns))
	}

	workflowPatterns := store.GetByType(PatternTypeWorkflow)
	if len(workflowPatterns) != 1 {
		t.Errorf("GetByType(Workflow) returned %d patterns, want 1", len(workflowPatterns))
	}
}

func TestGlobalPatternStore_GetForProject(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	patterns := []*GlobalPattern{
		{ID: "global-1", Type: PatternTypeCode, Title: "Global Pattern"},
		{ID: "project-1", Type: PatternTypeCode, Title: "Project A", Projects: []string{"/path/to/project-a"}},
		{ID: "project-2", Type: PatternTypeCode, Title: "Project B", Projects: []string{"/path/to/project-b"}},
	}

	for _, p := range patterns {
		if err := store.Add(p); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	projectAPatterns := store.GetForProject("/path/to/project-a")
	if len(projectAPatterns) != 2 { // global + project-a specific
		t.Errorf("GetForProject(project-a) returned %d patterns, want 2", len(projectAPatterns))
	}
}

func TestGlobalPatternStore_IncrementUse(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	pattern := &GlobalPattern{
		ID:    "test-pattern",
		Type:  PatternTypeCode,
		Title: "Test Pattern",
	}

	if err := store.Add(pattern); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := store.IncrementUse("test-pattern"); err != nil {
		t.Fatalf("IncrementUse() error = %v", err)
	}

	got, _ := store.Get("test-pattern")
	if got.Uses != 2 { // 1 from Add + 1 from IncrementUse
		t.Errorf("Uses = %d, want 2", got.Uses)
	}

	// Test increment for non-existent pattern
	err = store.IncrementUse("non-existent")
	if err == nil {
		t.Error("IncrementUse(non-existent) should return error")
	}
}

func TestGlobalPatternStore_Remove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	pattern := &GlobalPattern{
		ID:    "to-remove",
		Type:  PatternTypeCode,
		Title: "To Remove",
	}

	if err := store.Add(pattern); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if store.Count() != 1 {
		t.Errorf("Count() = %d, want 1", store.Count())
	}

	if err := store.Remove("to-remove"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if store.Count() != 0 {
		t.Errorf("Count() after Remove = %d, want 0", store.Count())
	}

	_, ok := store.Get("to-remove")
	if ok {
		t.Error("Get() after Remove should return false")
	}
}

func TestGlobalPatternStore_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create store and add pattern
	store1, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	pattern := &GlobalPattern{
		ID:          "persistent-pattern",
		Type:        PatternTypeWorkflow,
		Title:       "Persistent Pattern",
		Description: "Should persist across store instances",
	}

	if err := store1.Add(pattern); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Create new store instance and verify pattern loads
	store2, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() second instance error = %v", err)
	}

	got, ok := store2.Get("persistent-pattern")
	if !ok {
		t.Fatal("Pattern not found after reload")
	}

	if got.Title != "Persistent Pattern" {
		t.Errorf("Title = %q, want %q", got.Title, "Persistent Pattern")
	}
}

func TestGlobalPatternStore_UpdateExistingPattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	original := &GlobalPattern{
		ID:          "update-test",
		Type:        PatternTypeCode,
		Title:       "Original Title",
		Description: "Original description",
	}

	if err := store.Add(original); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got1, _ := store.Get("update-test")
	originalCreatedAt := got1.CreatedAt

	// Small delay to ensure UpdatedAt differs
	time.Sleep(10 * time.Millisecond)

	updated := &GlobalPattern{
		ID:          "update-test",
		Type:        PatternTypeCode,
		Title:       "Updated Title",
		Description: "Updated description",
	}

	if err := store.Add(updated); err != nil {
		t.Fatalf("Add() update error = %v", err)
	}

	got2, _ := store.Get("update-test")

	if got2.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", got2.Title, "Updated Title")
	}

	if got2.Uses != 2 { // Should increment on update
		t.Errorf("Uses = %d, want 2", got2.Uses)
	}

	if !got2.CreatedAt.Equal(originalCreatedAt) {
		t.Error("CreatedAt should not change on update")
	}

	if !got2.UpdatedAt.After(originalCreatedAt) {
		t.Error("UpdatedAt should be after CreatedAt")
	}
}

func TestPatternLearner_LearnFromExecution(t *testing.T) {
	// This test requires setting up the pattern extractor infrastructure
	// For now, verify the learner can be created and handles non-completed executions
	tmpDir, err := os.MkdirTemp("", "patterns-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	patternStore, err := NewGlobalPatternStore(tmpDir)
	if err != nil {
		t.Fatalf("NewGlobalPatternStore() error = %v", err)
	}

	// Create a minimal store for executions
	dbPath := tmpDir + "/test.db"
	execStore, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	defer execStore.Close()

	learner := NewPatternLearner(patternStore, execStore)
	if learner == nil {
		t.Fatal("NewPatternLearner() returned nil")
	}

	// Test with in-progress execution (should not learn)
	exec := &Execution{
		ID:     "test-exec",
		Status: "in_progress",
	}

	// Should return nil for non-completed executions
	err = learner.LearnFromExecution(nil, exec)
	if err != nil {
		t.Errorf("LearnFromExecution() for in_progress should not error, got %v", err)
	}
}

func TestPatternTypeConstants(t *testing.T) {
	// Verify pattern type constants are correctly defined
	types := map[PatternType]string{
		PatternTypeCode:      "code",
		PatternTypeStructure: "structure",
		PatternTypeNaming:    "naming",
		PatternTypeWorkflow:  "workflow",
		PatternTypeError:     "error",
	}

	for pType, expected := range types {
		if string(pType) != expected {
			t.Errorf("PatternType %v = %q, want %q", pType, pType, expected)
		}
	}
}

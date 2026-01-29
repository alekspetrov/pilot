package memory

import (
	"encoding/json"
	"os"
	"testing"
)

func TestNewKnowledgeGraph(t *testing.T) {
	t.Run("create new graph in empty directory", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "kg-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		kg, err := NewKnowledgeGraph(tmpDir)
		if err != nil {
			t.Errorf("NewKnowledgeGraph() error = %v", err)
			return
		}

		if kg == nil {
			t.Error("NewKnowledgeGraph() returned nil without error")
		}
	})

	t.Run("load existing graph", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "kg-test-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(tmpDir) }()

		// Create a graph file with pre-existing data
		nodes := []*GraphNode{
			{ID: "preexisting-1", Type: "test", Title: "Preexisting Node"},
		}
		data, _ := json.MarshalIndent(nodes, "", "  ")
		err = os.WriteFile(tmpDir+"/knowledge.json", data, 0644)
		if err != nil {
			t.Fatalf("failed to write test data: %v", err)
		}

		kg, err := NewKnowledgeGraph(tmpDir)
		if err != nil {
			t.Fatalf("NewKnowledgeGraph() error = %v", err)
		}

		if kg.Count() != 1 {
			t.Errorf("Count() = %d, want 1", kg.Count())
		}

		node, ok := kg.Get("preexisting-1")
		if !ok {
			t.Fatal("Get() returned false for preexisting node")
		}
		if node.Title != "Preexisting Node" {
			t.Errorf("Title = %q, want %q", node.Title, "Preexisting Node")
		}
	})
}

func TestKnowledgeGraph_AddAndGet(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	node := &GraphNode{
		ID:      "test-node-1",
		Type:    "test",
		Title:   "Test Node",
		Content: "Test content",
	}

	if err := kg.Add(node); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got, ok := kg.Get("test-node-1")
	if !ok {
		t.Fatal("Get() returned false, want true")
	}

	if got.Title != "Test Node" {
		t.Errorf("Title = %q, want %q", got.Title, "Test Node")
	}

	// Test Add without ID returns error
	noIDNode := &GraphNode{Type: "test", Title: "No ID"}
	if err := kg.Add(noIDNode); err == nil {
		t.Error("Add() with empty ID should return error")
	}
}

func TestKnowledgeGraph_Search(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	nodes := []*GraphNode{
		{ID: "1", Type: "pattern", Title: "Error Handling Pattern"},
		{ID: "2", Type: "learning", Title: "Testing Best Practices", Content: "Always test error handling"},
		{ID: "3", Type: "pattern", Title: "Logging Pattern"},
	}

	for _, n := range nodes {
		if err := kg.Add(n); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	// Search by title
	results := kg.Search("error")
	if len(results) != 2 { // Error Handling Pattern + Testing (content has "error handling")
		t.Errorf("Search(error) returned %d results, want 2", len(results))
	}

	// Search by type
	results = kg.Search("pattern")
	if len(results) != 2 {
		t.Errorf("Search(pattern) returned %d results, want 2", len(results))
	}

	// Case insensitive search
	results = kg.Search("LOGGING")
	if len(results) != 1 {
		t.Errorf("Search(LOGGING) returned %d results, want 1", len(results))
	}
}

func TestKnowledgeGraph_GetByType(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	nodes := []*GraphNode{
		{ID: "1", Type: "pattern", Title: "Pattern 1"},
		{ID: "2", Type: "pattern", Title: "Pattern 2"},
		{ID: "3", Type: "learning", Title: "Learning 1"},
	}

	for _, n := range nodes {
		if err := kg.Add(n); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	patterns := kg.GetByType("pattern")
	if len(patterns) != 2 {
		t.Errorf("GetByType(pattern) returned %d nodes, want 2", len(patterns))
	}

	learnings := kg.GetByType("learning")
	if len(learnings) != 1 {
		t.Errorf("GetByType(learning) returned %d nodes, want 1", len(learnings))
	}
}

func TestKnowledgeGraph_GetRelated(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	nodes := []*GraphNode{
		{ID: "main", Type: "pattern", Title: "Main Pattern", Relations: []string{"related-1", "related-2"}},
		{ID: "related-1", Type: "learning", Title: "Related Learning 1"},
		{ID: "related-2", Type: "learning", Title: "Related Learning 2"},
		{ID: "unrelated", Type: "pattern", Title: "Unrelated Pattern"},
	}

	for _, n := range nodes {
		if err := kg.Add(n); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	related := kg.GetRelated("main")
	if len(related) != 2 {
		t.Errorf("GetRelated(main) returned %d nodes, want 2", len(related))
	}

	// Non-existent node returns nil
	noRelated := kg.GetRelated("non-existent")
	if noRelated != nil {
		t.Error("GetRelated(non-existent) should return nil")
	}
}

func TestKnowledgeGraph_Remove(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	node := &GraphNode{ID: "to-remove", Type: "test", Title: "To Remove"}
	if err := kg.Add(node); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if kg.Count() != 1 {
		t.Errorf("Count() = %d, want 1", kg.Count())
	}

	if err := kg.Remove("to-remove"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if kg.Count() != 0 {
		t.Errorf("Count() after Remove = %d, want 0", kg.Count())
	}

	_, ok := kg.Get("to-remove")
	if ok {
		t.Error("Get() after Remove should return false")
	}
}

func TestKnowledgeGraph_Count(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("failed to create knowledge graph: %v", err)
	}

	if count := kg.Count(); count != 0 {
		t.Errorf("Count() = %d, want 0 for empty graph", count)
	}

	if err := kg.Add(&GraphNode{ID: "1", Type: "test", Title: "Node 1"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if count := kg.Count(); count != 1 {
		t.Errorf("Count() = %d, want 1", count)
	}
}

func TestKnowledgeGraph_AddPattern(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	metadata := map[string]interface{}{"language": "go"}
	if err := kg.AddPattern("error_handling", "Always check errors", metadata); err != nil {
		t.Fatalf("AddPattern() error = %v", err)
	}

	patterns := kg.GetPatterns()
	if len(patterns) != 1 {
		t.Errorf("GetPatterns() returned %d patterns, want 1", len(patterns))
	}

	if patterns[0].Type != "pattern" {
		t.Errorf("Type = %q, want %q", patterns[0].Type, "pattern")
	}

	if patterns[0].Content != "Always check errors" {
		t.Errorf("Content = %q, want %q", patterns[0].Content, "Always check errors")
	}
}

func TestKnowledgeGraph_AddLearning(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	metadata := map[string]interface{}{"source": "experience"}
	if err := kg.AddLearning("Context Usage", "Always pass context to functions", metadata); err != nil {
		t.Fatalf("AddLearning() error = %v", err)
	}

	learnings := kg.GetLearnings()
	if len(learnings) != 1 {
		t.Errorf("GetLearnings() returned %d learnings, want 1", len(learnings))
	}

	if learnings[0].Type != "learning" {
		t.Errorf("Type = %q, want %q", learnings[0].Type, "learning")
	}

	if learnings[0].Title != "Context Usage" {
		t.Errorf("Title = %q, want %q", learnings[0].Title, "Context Usage")
	}
}

func TestKnowledgeGraph_Persistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create graph and add node
	kg1, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	node := &GraphNode{
		ID:      "persistent-node",
		Type:    "pattern",
		Title:   "Persistent Node",
		Content: "Should persist across instances",
	}

	if err := kg1.Add(node); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Create new instance and verify node loads
	kg2, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() second instance error = %v", err)
	}

	got, ok := kg2.Get("persistent-node")
	if !ok {
		t.Fatal("Node not found after reload")
	}

	if got.Title != "Persistent Node" {
		t.Errorf("Title = %q, want %q", got.Title, "Persistent Node")
	}
}

func TestKnowledgeGraph_UpdateExistingNode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "kg-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	kg, err := NewKnowledgeGraph(tmpDir)
	if err != nil {
		t.Fatalf("NewKnowledgeGraph() error = %v", err)
	}

	original := &GraphNode{
		ID:      "update-test",
		Type:    "pattern",
		Title:   "Original Title",
		Content: "Original content",
	}

	if err := kg.Add(original); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	got1, _ := kg.Get("update-test")
	originalCreatedAt := got1.CreatedAt

	updated := &GraphNode{
		ID:      "update-test",
		Type:    "pattern",
		Title:   "Updated Title",
		Content: "Updated content",
	}

	if err := kg.Add(updated); err != nil {
		t.Fatalf("Add() update error = %v", err)
	}

	got2, _ := kg.Get("update-test")

	if got2.Title != "Updated Title" {
		t.Errorf("Title = %q, want %q", got2.Title, "Updated Title")
	}

	if !got2.CreatedAt.Equal(originalCreatedAt) {
		t.Error("CreatedAt should not change on update")
	}

	if !got2.UpdatedAt.After(originalCreatedAt) {
		t.Error("UpdatedAt should be after CreatedAt")
	}
}

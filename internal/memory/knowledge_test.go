package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeStore_InitSchema(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-knowledge-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	if err := ks.InitSchema(); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	// Verify table exists by inserting a memory
	m := &Memory{
		Type:    MemoryTypePattern,
		Content: "Test pattern",
	}
	if err := ks.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed after InitSchema: %v", err)
	}
}

func TestKnowledgeStore_AddAndGetMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memory
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "We use JWT for authentication",
		Context:    "auth-feature",
		Confidence: 0.9,
		ProjectID:  "project-1",
	}
	if err := ks.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}
	if m.ID == 0 {
		t.Error("Expected ID to be set after insert")
	}

	// Get memory
	retrieved, err := ks.GetMemory(m.ID)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}
	if retrieved.Type != MemoryTypePattern {
		t.Errorf("Expected type %s, got %s", MemoryTypePattern, retrieved.Type)
	}
	if retrieved.Content != "We use JWT for authentication" {
		t.Errorf("Unexpected content: %s", retrieved.Content)
	}
	if retrieved.Confidence != 0.9 {
		t.Errorf("Expected confidence 0.9, got %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_QueryByTopic(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "JWT authentication flow", ProjectID: "project-1"},
		{Type: MemoryTypePitfall, Content: "Auth tests break on token expiry", ProjectID: "project-1"},
		{Type: MemoryTypeDecision, Content: "Database uses PostgreSQL", ProjectID: "project-1"},
	}
	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Query by topic
	results, err := ks.QueryByTopic("auth", "project-1")
	if err != nil {
		t.Fatalf("QueryByTopic failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'auth', got %d", len(results))
	}

	// Query different topic
	results, err = ks.QueryByTopic("database", "project-1")
	if err != nil {
		t.Fatalf("QueryByTopic failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'database', got %d", len(results))
	}
}

func TestKnowledgeStore_QueryByType(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories of different types
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "Pattern 1", ProjectID: "p1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "Pattern 2", ProjectID: "p1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePitfall, Content: "Pitfall 1", ProjectID: "p1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypeLearning, Content: "Learning 1", ProjectID: "p1"})

	// Query patterns
	results, err := ks.QueryByType(MemoryTypePattern, "p1")
	if err != nil {
		t.Fatalf("QueryByType failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 patterns, got %d", len(results))
	}

	// Query pitfalls
	results, err = ks.QueryByType(MemoryTypePitfall, "p1")
	if err != nil {
		t.Fatalf("QueryByType failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 pitfall, got %d", len(results))
	}
}

func TestKnowledgeStore_DecayConfidence(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memory with high confidence
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Test decay",
		Confidence: 1.0,
	}
	_ = ks.AddMemory(m)

	// Apply decay (rate = 0.1 per day)
	if err := ks.DecayConfidence(0.1); err != nil {
		t.Fatalf("DecayConfidence failed: %v", err)
	}

	// Confidence should have decayed slightly (same day, small decay)
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Confidence > 1.0 {
		t.Errorf("Confidence should not increase: %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_PruneStale(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories with varying confidence
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "High confidence", Confidence: 0.9})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "Low confidence", Confidence: 0.05})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "Medium confidence", Confidence: 0.5})

	// Prune memories below 0.1
	if err := ks.PruneStale(0.1); err != nil {
		t.Fatalf("PruneStale failed: %v", err)
	}

	// Should have 2 memories left
	count, _ := ks.Count()
	if count != 2 {
		t.Errorf("Expected 2 memories after prune, got %d", count)
	}
}

func TestKnowledgeStore_ReinforceMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memory
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Reinforce me",
		Confidence: 0.5,
	}
	_ = ks.AddMemory(m)

	// Reinforce
	if err := ks.ReinforceMemory(m.ID, 0.2); err != nil {
		t.Fatalf("ReinforceMemory failed: %v", err)
	}

	// Check increased confidence
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Confidence != 0.7 {
		t.Errorf("Expected confidence 0.7, got %f", retrieved.Confidence)
	}

	// Reinforce to max
	_ = ks.ReinforceMemory(m.ID, 0.5)
	retrieved, _ = ks.GetMemory(m.ID)
	if retrieved.Confidence > 1.0 {
		t.Errorf("Confidence should cap at 1.0, got %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_CountByType(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories of different types
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "P1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "P2"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePitfall, Content: "Pit1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypeDecision, Content: "D1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypeLearning, Content: "L1"})
	_ = ks.AddMemory(&Memory{Type: MemoryTypeLearning, Content: "L2"})

	counts, err := ks.CountByType()
	if err != nil {
		t.Fatalf("CountByType failed: %v", err)
	}

	if counts[MemoryTypePattern] != 2 {
		t.Errorf("Expected 2 patterns, got %d", counts[MemoryTypePattern])
	}
	if counts[MemoryTypePitfall] != 1 {
		t.Errorf("Expected 1 pitfall, got %d", counts[MemoryTypePitfall])
	}
	if counts[MemoryTypeLearning] != 2 {
		t.Errorf("Expected 2 learnings, got %d", counts[MemoryTypeLearning])
	}
}

func TestKnowledgeStore_SyncToFiles(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories
	_ = ks.AddMemory(&Memory{Type: MemoryTypePattern, Content: "Pattern for sync", Confidence: 0.8})
	_ = ks.AddMemory(&Memory{Type: MemoryTypePitfall, Content: "Pitfall for sync", Confidence: 0.7})

	// Sync to files
	agentPath := filepath.Join(tmpDir, ".agent")
	if err := ks.SyncToFiles(agentPath); err != nil {
		t.Fatalf("SyncToFiles failed: %v", err)
	}

	// Verify files created
	patternsFile := filepath.Join(agentPath, "memories", "patterns.md")
	if _, err := os.Stat(patternsFile); os.IsNotExist(err) {
		t.Error("patterns.md not created")
	}

	pitfallsFile := filepath.Join(agentPath, "memories", "pitfalls.md")
	if _, err := os.Stat(pitfallsFile); os.IsNotExist(err) {
		t.Error("pitfalls.md not created")
	}

	// Verify content
	content, _ := os.ReadFile(patternsFile)
	if !containsSubstr(string(content), "Pattern for sync") {
		t.Error("patterns.md missing expected content")
	}
}

func TestKnowledgeStore_UpdateMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memory
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Original content",
		Confidence: 0.5,
	}
	_ = ks.AddMemory(m)

	// Update memory
	m.Content = "Updated content"
	m.Confidence = 0.8
	if err := ks.UpdateMemory(m); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	// Verify update
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Content != "Updated content" {
		t.Errorf("Expected 'Updated content', got '%s'", retrieved.Content)
	}
	if retrieved.Confidence != 0.8 {
		t.Errorf("Expected confidence 0.8, got %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_DeleteMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memory
	m := &Memory{Type: MemoryTypePattern, Content: "To be deleted"}
	_ = ks.AddMemory(m)

	// Delete
	if err := ks.DeleteMemory(m.ID); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	// Verify deleted
	_, err := ks.GetMemory(m.ID)
	if err == nil {
		t.Error("Expected error when getting deleted memory")
	}
}

func TestMemoryTypes(t *testing.T) {
	// Verify memory type constants
	if MemoryTypePattern != "pattern" {
		t.Errorf("Expected 'pattern', got '%s'", MemoryTypePattern)
	}
	if MemoryTypePitfall != "pitfall" {
		t.Errorf("Expected 'pitfall', got '%s'", MemoryTypePitfall)
	}
	if MemoryTypeDecision != "decision" {
		t.Errorf("Expected 'decision', got '%s'", MemoryTypeDecision)
	}
	if MemoryTypeLearning != "learning" {
		t.Errorf("Expected 'learning', got '%s'", MemoryTypeLearning)
	}
}

func containsSubstr(s, substr string) bool {
	return strings.Contains(s, substr)
}

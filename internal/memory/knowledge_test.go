package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupKnowledgeTestDB(t *testing.T) (*KnowledgeStore, func()) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "knowledge-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open database: %v", err)
	}

	store := NewKnowledgeStore(db)
	if err := store.InitSchema(); err != nil {
		db.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to init schema: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestKnowledgeStore_AddAndGetMemory(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "We use JWT for authentication",
		Context:    "auth/middleware.go",
		Confidence: 0.9,
		ProjectID:  "pilot",
	}

	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	if m.ID == 0 {
		t.Error("expected memory ID to be set after insert")
	}

	retrieved, err := store.GetMemory(m.ID)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}

	if retrieved.Type != MemoryTypePattern {
		t.Errorf("expected type %s, got %s", MemoryTypePattern, retrieved.Type)
	}
	if retrieved.Content != m.Content {
		t.Errorf("expected content %q, got %q", m.Content, retrieved.Content)
	}
	if retrieved.ProjectID != "pilot" {
		t.Errorf("expected projectID 'pilot', got %q", retrieved.ProjectID)
	}
}

func TestKnowledgeStore_QueryByTopic(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Use JWT for auth", Context: "auth/", Confidence: 0.9, ProjectID: "pilot"},
		{Type: MemoryTypePitfall, Content: "Auth changes break sessions", Context: "auth/session.go", Confidence: 0.8, ProjectID: "pilot"},
		{Type: MemoryTypeDecision, Content: "Chose Redis for caching", Context: "cache/", Confidence: 0.7, ProjectID: "pilot"},
	}

	for _, m := range memories {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
	}

	results, err := store.QueryByTopic("auth", "pilot")
	if err != nil {
		t.Fatalf("QueryByTopic failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	// Should be ordered by confidence DESC
	if len(results) >= 2 && results[0].Confidence < results[1].Confidence {
		t.Error("expected results ordered by confidence DESC")
	}
}

func TestKnowledgeStore_QueryByType(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	memories := []*Memory{
		{Type: MemoryTypePitfall, Content: "Pitfall 1", Confidence: 0.9, ProjectID: "proj1"},
		{Type: MemoryTypePitfall, Content: "Pitfall 2", Confidence: 0.8, ProjectID: "proj1"},
		{Type: MemoryTypePattern, Content: "Pattern 1", Confidence: 0.9, ProjectID: "proj1"},
	}

	for _, m := range memories {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
	}

	results, err := store.QueryByType(MemoryTypePitfall, "proj1")
	if err != nil {
		t.Fatalf("QueryByType failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 pitfalls, got %d", len(results))
	}
}

func TestKnowledgeStore_UpdateMemory(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypeLearning,
		Content:    "Original content",
		Confidence: 0.5,
	}
	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	m.Content = "Updated content"
	m.Confidence = 0.8
	if err := store.UpdateMemory(m); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	retrieved, err := store.GetMemory(m.ID)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}

	if retrieved.Content != "Updated content" {
		t.Errorf("expected updated content, got %q", retrieved.Content)
	}
	if retrieved.Confidence != 0.8 {
		t.Errorf("expected confidence 0.8, got %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_ReinforceMemory(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Test pattern",
		Confidence: 0.7,
	}
	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	if err := store.ReinforceMemory(m.ID, 0.1); err != nil {
		t.Fatalf("ReinforceMemory failed: %v", err)
	}

	retrieved, err := store.GetMemory(m.ID)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}

	if retrieved.Confidence < 0.79 || retrieved.Confidence > 0.81 {
		t.Errorf("expected confidence ~0.8 after reinforcement, got %f", retrieved.Confidence)
	}

	// Test cap at 1.0
	if err := store.ReinforceMemory(m.ID, 0.5); err != nil {
		t.Fatalf("ReinforceMemory failed: %v", err)
	}

	retrieved, _ = store.GetMemory(m.ID)
	if retrieved.Confidence > 1.0 {
		t.Errorf("expected confidence capped at 1.0, got %f", retrieved.Confidence)
	}
}

func TestKnowledgeStore_DecayConfidence(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	// Insert memory with old timestamp
	_, err := store.db.Exec(`
		INSERT INTO memories (type, content, confidence, updated_at)
		VALUES (?, ?, ?, ?)
	`, MemoryTypePattern, "Old pattern", 0.9, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("failed to insert test memory: %v", err)
	}

	// Decay with rate of 0.1 per day
	if err := store.DecayConfidence(0.1); err != nil {
		t.Fatalf("DecayConfidence failed: %v", err)
	}

	rows, err := store.db.Query("SELECT confidence FROM memories")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var conf float64
		rows.Scan(&conf)
		if conf >= 0.9 {
			t.Errorf("expected confidence to decay from 0.9, still %f", conf)
		}
	}
}

func TestKnowledgeStore_PruneStale(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "High confidence", Confidence: 0.9},
		{Type: MemoryTypePattern, Content: "Low confidence", Confidence: 0.05},
		{Type: MemoryTypePattern, Content: "Very low", Confidence: 0.01},
	}

	for _, m := range memories {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
	}

	if err := store.PruneStale(0.1); err != nil {
		t.Fatalf("PruneStale failed: %v", err)
	}

	var count int
	store.db.QueryRow("SELECT COUNT(*) FROM memories").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 memory after prune, got %d", count)
	}
}

func TestKnowledgeStore_DeleteMemory(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{Type: MemoryTypePattern, Content: "To delete", Confidence: 0.9}
	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	if err := store.DeleteMemory(m.ID); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	_, err := store.GetMemory(m.ID)
	if err != sql.ErrNoRows {
		t.Errorf("expected ErrNoRows after delete, got %v", err)
	}
}

func TestKnowledgeStore_GetRecentMemories(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		m := &Memory{Type: MemoryTypePattern, Content: "Pattern", Confidence: 0.9}
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
	}

	results, err := store.GetRecentMemories(3)
	if err != nil {
		t.Fatalf("GetRecentMemories failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestKnowledgeStore_CountByType(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "P1", Confidence: 0.9, ProjectID: "proj"},
		{Type: MemoryTypePattern, Content: "P2", Confidence: 0.9, ProjectID: "proj"},
		{Type: MemoryTypePitfall, Content: "F1", Confidence: 0.9, ProjectID: "proj"},
		{Type: MemoryTypeDecision, Content: "D1", Confidence: 0.9, ProjectID: "proj"},
	}

	for _, m := range memories {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
	}

	counts, err := store.CountByType("proj")
	if err != nil {
		t.Fatalf("CountByType failed: %v", err)
	}

	if counts[MemoryTypePattern] != 2 {
		t.Errorf("expected 2 patterns, got %d", counts[MemoryTypePattern])
	}
	if counts[MemoryTypePitfall] != 1 {
		t.Errorf("expected 1 pitfall, got %d", counts[MemoryTypePitfall])
	}
}

func TestMemoryTypes(t *testing.T) {
	tests := []struct {
		memType  MemoryType
		expected string
	}{
		{MemoryTypePattern, "pattern"},
		{MemoryTypePitfall, "pitfall"},
		{MemoryTypeDecision, "decision"},
		{MemoryTypeLearning, "learning"},
	}

	for _, tt := range tests {
		if string(tt.memType) != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.memType)
		}
	}
}

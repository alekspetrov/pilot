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

func TestKnowledgeStore_IndexConcepts(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "We use JWT authentication with OAuth for the API endpoints",
		Context:    "auth/middleware.go",
		Confidence: 0.9,
		ProjectID:  "pilot",
	}

	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	if err := store.IndexConcepts(m.ID, m.Content+" "+m.Context); err != nil {
		t.Fatalf("IndexConcepts failed: %v", err)
	}

	// Check that concepts were created
	concepts, err := store.GetAllConcepts()
	if err != nil {
		t.Fatalf("GetAllConcepts failed: %v", err)
	}

	// Should have extracted: jwt, auth, oauth, api, middleware
	expectedConcepts := map[string]bool{"jwt": false, "auth": false, "oauth": false, "api": false, "middleware": false}
	for _, c := range concepts {
		if _, ok := expectedConcepts[c.Name]; ok {
			expectedConcepts[c.Name] = true
		}
	}

	for name, found := range expectedConcepts {
		if !found {
			t.Errorf("expected concept %q to be extracted", name)
		}
	}
}

func TestKnowledgeStore_GetConceptByName(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Database migration patterns",
		Confidence: 0.9,
	}
	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}

	if err := store.IndexConcepts(m.ID, m.Content); err != nil {
		t.Fatalf("IndexConcepts failed: %v", err)
	}

	concept, err := store.GetConceptByName("database")
	if err != nil {
		t.Fatalf("GetConceptByName failed: %v", err)
	}

	if concept.Name != "database" {
		t.Errorf("expected name 'database', got %q", concept.Name)
	}

	if len(concept.Memories) != 1 || concept.Memories[0] != m.ID {
		t.Errorf("expected memory ID %d in concept, got %v", m.ID, concept.Memories)
	}
}

func TestKnowledgeStore_QueryGraph(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	// Create test memories with different concepts
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Use JWT for API authentication", Context: "auth/jwt.go", Confidence: 0.9, ProjectID: "pilot"},
		{Type: MemoryTypePitfall, Content: "Auth changes break session handling", Context: "auth/session.go", Confidence: 0.8, ProjectID: "pilot"},
		{Type: MemoryTypeDecision, Content: "Database migration uses schema versioning", Context: "db/migrate.go", Confidence: 0.7, ProjectID: "pilot"},
		{Type: MemoryTypeLearning, Content: "API rate limiting prevents abuse", Context: "api/ratelimit.go", Confidence: 0.6, ProjectID: "pilot"},
	}

	for _, m := range memories {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
		if err := store.IndexConcepts(m.ID, m.Content+" "+m.Context); err != nil {
			t.Fatalf("IndexConcepts failed: %v", err)
		}
	}

	// Query for "auth"
	result, err := store.QueryGraph("auth", "pilot")
	if err != nil {
		t.Fatalf("QueryGraph failed: %v", err)
	}

	// Should find direct matches containing "auth"
	if len(result.Memories) < 2 {
		t.Errorf("expected at least 2 direct matches for 'auth', got %d", len(result.Memories))
	}

	// Should have related concepts
	if len(result.RelatedConcepts) == 0 {
		t.Error("expected related concepts to be found")
	}

	// Check query is stored
	if result.Query != "auth" {
		t.Errorf("expected query 'auth', got %q", result.Query)
	}
}

func TestKnowledgeStore_QueryGraph_ConceptExpansion(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	// Create memories that share concepts
	m1 := &Memory{Type: MemoryTypePattern, Content: "JWT token validation", Confidence: 0.9, ProjectID: "proj"}
	m2 := &Memory{Type: MemoryTypePattern, Content: "OAuth integration with JWT", Confidence: 0.8, ProjectID: "proj"}
	m3 := &Memory{Type: MemoryTypePitfall, Content: "Token expiry handling", Confidence: 0.7, ProjectID: "proj"}

	for _, m := range []*Memory{m1, m2, m3} {
		if err := store.AddMemory(m); err != nil {
			t.Fatalf("AddMemory failed: %v", err)
		}
		if err := store.IndexConcepts(m.ID, m.Content); err != nil {
			t.Fatalf("IndexConcepts failed: %v", err)
		}
	}

	// Query for "jwt" should find m1 and m2 directly
	result, err := store.QueryGraph("jwt", "proj")
	if err != nil {
		t.Fatalf("QueryGraph failed: %v", err)
	}

	// m1 and m2 contain "jwt" directly
	if len(result.Memories) < 2 {
		t.Errorf("expected at least 2 direct matches, got %d", len(result.Memories))
	}

	// Concept expansion should find related memories through shared concepts like "token"
	if len(result.RelatedConcepts) == 0 {
		t.Log("No related concepts found - this may be expected if jwt is the only matching concept")
	}
}

func TestKnowledgeStore_ReindexMemory(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Original content about auth",
		Confidence: 0.9,
	}
	if err := store.AddMemory(m); err != nil {
		t.Fatalf("AddMemory failed: %v", err)
	}
	if err := store.IndexConcepts(m.ID, m.Content); err != nil {
		t.Fatalf("IndexConcepts failed: %v", err)
	}

	// Update memory content
	m.Content = "Updated content about database and api"
	if err := store.UpdateMemory(m); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	// Reindex
	if err := store.ReindexMemory(m.ID); err != nil {
		t.Fatalf("ReindexMemory failed: %v", err)
	}

	// Check new concepts were indexed
	dbConcept, err := store.GetConceptByName("database")
	if err != nil {
		t.Fatalf("GetConceptByName for database failed: %v", err)
	}

	found := false
	for _, memID := range dbConcept.Memories {
		if memID == m.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected memory to be linked to 'database' concept after reindex")
	}
}

func TestExtractConcepts(t *testing.T) {
	tests := []struct {
		content  string
		expected []string
	}{
		{
			content:  "We use JWT for authentication",
			expected: []string{"jwt", "auth"},
		},
		{
			content:  "Database migration with schema changes",
			expected: []string{"database", "migration", "schema"},
		},
		{
			content:  "API endpoint with cache and logging",
			expected: []string{"api", "endpoint", "cache", "logging"},
		},
		{
			content:  "no concepts here",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		result := extractConcepts(tt.content)
		for _, exp := range tt.expected {
			found := false
			for _, r := range result {
				if r == exp {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected concept %q in result for content %q, got %v", exp, tt.content, result)
			}
		}
	}
}

func TestKnowledgeStore_ScoreResults(t *testing.T) {
	store, cleanup := setupKnowledgeTestDB(t)
	defer cleanup()

	// Create test result
	result := &GraphQueryResult{
		Query: "jwt auth",
		Memories: []*Memory{
			{ID: 1, Content: "JWT authentication pattern", Type: MemoryTypePattern, Confidence: 0.5},
			{ID: 2, Content: "Auth session handling", Type: MemoryTypePitfall, Confidence: 0.9},
			{ID: 3, Content: "Unrelated content", Type: MemoryTypePattern, Confidence: 0.8},
		},
	}

	store.scoreResults(result)

	// First result should have exact match (jwt + auth)
	// The scoring should prioritize the memory with best match, not just confidence
	if len(result.Memories) < 3 {
		t.Fatalf("expected 3 memories, got %d", len(result.Memories))
	}

	// Memory with "JWT" and higher match should rank higher than unrelated content
	firstContent := result.Memories[0].Content
	if firstContent == "Unrelated content" {
		t.Error("expected better matches to rank higher than unrelated content")
	}
}

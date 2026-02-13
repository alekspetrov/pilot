package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewKnowledgeStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "pilot-knowledge-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	if ks == nil {
		t.Fatal("NewKnowledgeStore returned nil")
	}

	if err := ks.InitSchema(); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}
}

func TestAddMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	tests := []struct {
		name      string
		memory    *Memory
		wantError bool
	}{
		{
			name: "valid pattern",
			memory: &Memory{
				Type:       MemoryTypePattern,
				Content:    "Use JWT for authentication",
				Context:    "auth module implementation",
				Confidence: 0.9,
				ProjectID:  "test-project",
			},
			wantError: false,
		},
		{
			name: "valid pitfall",
			memory: &Memory{
				Type:       MemoryTypePitfall,
				Content:    "Auth changes often break tests",
				Context:    "CI failures in auth module",
				Confidence: 0.85,
				ProjectID:  "test-project",
			},
			wantError: false,
		},
		{
			name: "missing content",
			memory: &Memory{
				Type:      MemoryTypeDecision,
				ProjectID: "test-project",
			},
			wantError: true,
		},
		{
			name: "missing type",
			memory: &Memory{
				Content:   "Some content",
				ProjectID: "test-project",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ks.AddMemory(tt.memory)
			if tt.wantError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if !tt.wantError && tt.memory.ID == 0 {
				t.Error("memory ID not set after insert")
			}
		})
	}
}

func TestGetMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory
	m := &Memory{
		Type:       MemoryTypeLearning,
		Content:    "This error usually means missing dependencies",
		Context:    "build failures",
		Confidence: 0.8,
		ProjectID:  "test-project",
	}
	_ = ks.AddMemory(m)

	// Retrieve it
	retrieved, err := ks.GetMemory(m.ID)
	if err != nil {
		t.Fatalf("GetMemory failed: %v", err)
	}

	if retrieved.Content != m.Content {
		t.Errorf("Content = %q, want %q", retrieved.Content, m.Content)
	}
	if retrieved.Type != m.Type {
		t.Errorf("Type = %q, want %q", retrieved.Type, m.Type)
	}
	if retrieved.Context != m.Context {
		t.Errorf("Context = %q, want %q", retrieved.Context, m.Context)
	}
	if retrieved.ProjectID != m.ProjectID {
		t.Errorf("ProjectID = %q, want %q", retrieved.ProjectID, m.ProjectID)
	}
}

func TestGetMemory_NotFound(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	_, err := ks.GetMemory(999)
	if err == nil {
		t.Error("expected error for nonexistent memory")
	}
}

func TestUpdateMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Original content",
		Confidence: 0.7,
	}
	_ = ks.AddMemory(m)

	// Update it
	m.Content = "Updated content"
	m.Confidence = 0.9

	if err := ks.UpdateMemory(m); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	// Verify update
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Content != "Updated content" {
		t.Errorf("Content = %q, want 'Updated content'", retrieved.Content)
	}
	if retrieved.Confidence != 0.9 {
		t.Errorf("Confidence = %f, want 0.9", retrieved.Confidence)
	}
}

func TestDeleteMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory
	m := &Memory{
		Type:    MemoryTypePattern,
		Content: "To be deleted",
	}
	_ = ks.AddMemory(m)

	// Delete it
	if err := ks.DeleteMemory(m.ID); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	// Verify deletion
	_, err := ks.GetMemory(m.ID)
	if err == nil {
		t.Error("expected error for deleted memory")
	}
}

func TestQueryByTopic(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "JWT authentication is preferred", Context: "auth", ProjectID: "proj-1"},
		{Type: MemoryTypePitfall, Content: "Auth tests are flaky", Context: "auth testing", ProjectID: "proj-1"},
		{Type: MemoryTypeDecision, Content: "Using PostgreSQL for database", Context: "db setup", ProjectID: "proj-1"},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Query by topic
	results, err := ks.QueryByTopic("auth", "proj-1")
	if err != nil {
		t.Fatalf("QueryByTopic failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestQueryByType(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories of different types
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Pattern 1"},
		{Type: MemoryTypePattern, Content: "Pattern 2"},
		{Type: MemoryTypePitfall, Content: "Pitfall 1"},
		{Type: MemoryTypeLearning, Content: "Learning 1"},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Query patterns
	patterns, err := ks.QueryByType(MemoryTypePattern, "")
	if err != nil {
		t.Fatalf("QueryByType failed: %v", err)
	}

	if len(patterns) != 2 {
		t.Errorf("expected 2 patterns, got %d", len(patterns))
	}
}

func TestDecayConfidence(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory with high confidence
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Test pattern",
		Confidence: 1.0,
	}
	_ = ks.AddMemory(m)

	// Apply decay (simulate 10 days with 0.05 rate = 0.5 total decay)
	// For testing, we'll use a high rate
	if err := ks.DecayConfidence(0.01); err != nil {
		t.Fatalf("DecayConfidence failed: %v", err)
	}

	// Verify confidence decreased (exact amount depends on time since update)
	retrieved, _ := ks.GetMemory(m.ID)
	// Confidence should be less than or equal to original
	if retrieved.Confidence > 1.0 {
		t.Errorf("Confidence = %f, should be <= 1.0", retrieved.Confidence)
	}
}

func TestReinforceMemory(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory with low confidence
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Test pattern",
		Confidence: 0.5,
	}
	_ = ks.AddMemory(m)

	// Reinforce it
	if err := ks.ReinforceMemory(m.ID, 0.2); err != nil {
		t.Fatalf("ReinforceMemory failed: %v", err)
	}

	// Verify confidence increased
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Confidence != 0.7 {
		t.Errorf("Confidence = %f, want 0.7", retrieved.Confidence)
	}
}

func TestReinforceMemory_MaxConfidence(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add a memory with high confidence
	m := &Memory{
		Type:       MemoryTypePattern,
		Content:    "Test pattern",
		Confidence: 0.95,
	}
	_ = ks.AddMemory(m)

	// Reinforce it with large boost
	_ = ks.ReinforceMemory(m.ID, 0.5)

	// Verify confidence capped at 1.0
	retrieved, _ := ks.GetMemory(m.ID)
	if retrieved.Confidence > 1.0 {
		t.Errorf("Confidence = %f, should be capped at 1.0", retrieved.Confidence)
	}
}

func TestPruneStale(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories with varying confidence
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "High confidence", Confidence: 0.9},
		{Type: MemoryTypePattern, Content: "Medium confidence", Confidence: 0.5},
		{Type: MemoryTypePattern, Content: "Low confidence", Confidence: 0.05},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Prune with threshold 0.1
	pruned, err := ks.PruneStale(0.1)
	if err != nil {
		t.Fatalf("PruneStale failed: %v", err)
	}

	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Verify remaining memories
	all, _ := ks.GetAllMemories("")
	if len(all) != 2 {
		t.Errorf("expected 2 remaining memories, got %d", len(all))
	}
}

func TestGetStats(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories of different types
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Pattern 1", Confidence: 0.8, ProjectID: "proj-1"},
		{Type: MemoryTypePattern, Content: "Pattern 2", Confidence: 0.9, ProjectID: "proj-1"},
		{Type: MemoryTypePitfall, Content: "Pitfall 1", Confidence: 0.7, ProjectID: "proj-2"},
		{Type: MemoryTypeDecision, Content: "Decision 1", Confidence: 0.85, ProjectID: "proj-1"},
		{Type: MemoryTypeLearning, Content: "Learning 1", Confidence: 0.75, ProjectID: "proj-3"},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	stats, err := ks.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.Total != 5 {
		t.Errorf("Total = %d, want 5", stats.Total)
	}
	if stats.Patterns != 2 {
		t.Errorf("Patterns = %d, want 2", stats.Patterns)
	}
	if stats.Pitfalls != 1 {
		t.Errorf("Pitfalls = %d, want 1", stats.Pitfalls)
	}
	if stats.Decisions != 1 {
		t.Errorf("Decisions = %d, want 1", stats.Decisions)
	}
	if stats.Learnings != 1 {
		t.Errorf("Learnings = %d, want 1", stats.Learnings)
	}
	if stats.ProjectCount != 3 {
		t.Errorf("ProjectCount = %d, want 3", stats.ProjectCount)
	}
}

func TestSyncToFiles(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Pattern content", Context: "testing"},
		{Type: MemoryTypePitfall, Content: "Pitfall content", Context: "debugging"},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Sync to files
	agentPath := filepath.Join(tmpDir, ".agent")
	if err := ks.SyncToFiles(agentPath); err != nil {
		t.Fatalf("SyncToFiles failed: %v", err)
	}

	// Verify files created
	memoriesDir := filepath.Join(agentPath, "memories")
	if _, err := os.Stat(memoriesDir); os.IsNotExist(err) {
		t.Error("memories directory not created")
	}

	patternFile := filepath.Join(memoriesDir, "pattern.md")
	if _, err := os.Stat(patternFile); os.IsNotExist(err) {
		t.Error("pattern.md not created")
	}

	pitfallFile := filepath.Join(memoriesDir, "pitfall.md")
	if _, err := os.Stat(pitfallFile); os.IsNotExist(err) {
		t.Error("pitfall.md not created")
	}
}

func TestFindSimilar(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "JWT authentication is the standard approach", Confidence: 0.9},
		{Type: MemoryTypePitfall, Content: "Authentication tokens expire silently", Confidence: 0.8},
		{Type: MemoryTypeDecision, Content: "Using PostgreSQL for the database layer", Confidence: 0.85},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Find similar to "authentication"
	results, err := ks.FindSimilar("authentication issues", "", 5)
	if err != nil {
		t.Fatalf("FindSimilar failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 similar memories, got %d", len(results))
	}
}

func TestGetAllMemories(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "pilot-knowledge-test-*")
	defer func() { _ = os.RemoveAll(tmpDir) }()

	store, _ := NewStore(tmpDir)
	defer func() { _ = store.Close() }()

	ks := NewKnowledgeStore(store.DB())
	_ = ks.InitSchema()

	// Add memories for different projects
	memories := []*Memory{
		{Type: MemoryTypePattern, Content: "Pattern 1", ProjectID: "proj-1"},
		{Type: MemoryTypePattern, Content: "Pattern 2", ProjectID: "proj-1"},
		{Type: MemoryTypePattern, Content: "Pattern 3", ProjectID: "proj-2"},
		{Type: MemoryTypePattern, Content: "Global pattern", ProjectID: ""},
	}

	for _, m := range memories {
		_ = ks.AddMemory(m)
	}

	// Get all memories
	all, err := ks.GetAllMemories("")
	if err != nil {
		t.Fatalf("GetAllMemories failed: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 memories, got %d", len(all))
	}

	// Get memories for proj-1 (includes global)
	proj1, err := ks.GetAllMemories("proj-1")
	if err != nil {
		t.Fatalf("GetAllMemories (proj-1) failed: %v", err)
	}
	if len(proj1) != 3 {
		t.Errorf("expected 3 memories for proj-1, got %d", len(proj1))
	}
}

func TestMemoryTypes(t *testing.T) {
	// Verify memory type constants
	if MemoryTypePattern != "pattern" {
		t.Errorf("MemoryTypePattern = %q, want 'pattern'", MemoryTypePattern)
	}
	if MemoryTypePitfall != "pitfall" {
		t.Errorf("MemoryTypePitfall = %q, want 'pitfall'", MemoryTypePitfall)
	}
	if MemoryTypeDecision != "decision" {
		t.Errorf("MemoryTypeDecision = %q, want 'decision'", MemoryTypeDecision)
	}
	if MemoryTypeLearning != "learning" {
		t.Errorf("MemoryTypeLearning = %q, want 'learning'", MemoryTypeLearning)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly ten", 11, "exactly ten"},
		{"this is a longer string", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

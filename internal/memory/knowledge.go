package memory

import (
	"database/sql"
	"sort"
	"strings"
	"time"
)

// MemoryType categorizes experiential memories
type MemoryType string

const (
	MemoryTypePattern  MemoryType = "pattern"  // "We use JWT for auth"
	MemoryTypePitfall  MemoryType = "pitfall"  // "Auth changes break tests"
	MemoryTypeDecision MemoryType = "decision" // "JWT over sessions for scaling"
	MemoryTypeLearning MemoryType = "learning" // "This error usually means X"
)

// Memory represents an experiential memory entry
type Memory struct {
	ID         int64
	Type       MemoryType
	Content    string
	Context    string  // Task/file where this was learned
	Confidence float64 // 0.0-1.0, decays over time
	ProjectID  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Concept represents an indexed concept extracted from memory content
type Concept struct {
	ID       int64
	Name     string  // "auth", "api", "testing"
	Memories []int64 // Memory IDs linked to this concept
}

// GraphQueryResult holds search results with context from concept expansion
type GraphQueryResult struct {
	Memories        []*Memory
	RelatedConcepts []*Concept
	RelatedMemories []*Memory
	Query           string
}

// KnowledgeStore manages experiential memories
type KnowledgeStore struct {
	db *sql.DB
}

// NewKnowledgeStore creates a knowledge store
func NewKnowledgeStore(db *sql.DB) *KnowledgeStore {
	return &KnowledgeStore{db: db}
}

// InitSchema creates the memories and concepts tables
func (k *KnowledgeStore) InitSchema() error {
	_, err := k.db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL,
			content TEXT NOT NULL,
			context TEXT,
			confidence REAL DEFAULT 1.0,
			project_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type);
		CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id);

		CREATE TABLE IF NOT EXISTS concepts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE NOT NULL
		);

		CREATE TABLE IF NOT EXISTS memory_concepts (
			memory_id INTEGER NOT NULL,
			concept_id INTEGER NOT NULL,
			PRIMARY KEY (memory_id, concept_id),
			FOREIGN KEY (memory_id) REFERENCES memories(id) ON DELETE CASCADE,
			FOREIGN KEY (concept_id) REFERENCES concepts(id) ON DELETE CASCADE
		);
		CREATE INDEX IF NOT EXISTS idx_memory_concepts_memory ON memory_concepts(memory_id);
		CREATE INDEX IF NOT EXISTS idx_memory_concepts_concept ON memory_concepts(concept_id);
	`)
	return err
}

// AddMemory stores a new memory
func (k *KnowledgeStore) AddMemory(m *Memory) error {
	result, err := k.db.Exec(`
		INSERT INTO memories (type, content, context, confidence, project_id)
		VALUES (?, ?, ?, ?, ?)
	`, m.Type, m.Content, m.Context, m.Confidence, m.ProjectID)
	if err != nil {
		return err
	}
	m.ID, _ = result.LastInsertId()
	return nil
}

// GetMemory retrieves a memory by ID
func (k *KnowledgeStore) GetMemory(id int64) (*Memory, error) {
	row := k.db.QueryRow(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories WHERE id = ?
	`, id)

	m := &Memory{}
	var projectID sql.NullString
	err := row.Scan(&m.ID, &m.Type, &m.Content, &m.Context,
		&m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if projectID.Valid {
		m.ProjectID = projectID.String
	}
	return m, nil
}

// QueryByTopic searches memories by content/context
func (k *KnowledgeStore) QueryByTopic(topic string, projectID string) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE (content LIKE ? OR context LIKE ?)
		AND (project_id = ? OR project_id IS NULL OR project_id = '')
		AND confidence > 0.1
		ORDER BY confidence DESC, updated_at DESC
		LIMIT 10
	`, "%"+topic+"%", "%"+topic+"%", projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// QueryByType retrieves memories of a specific type
func (k *KnowledgeStore) QueryByType(memType MemoryType, projectID string) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE type = ?
		AND (project_id = ? OR project_id IS NULL OR project_id = '')
		AND confidence > 0.1
		ORDER BY confidence DESC, updated_at DESC
		LIMIT 20
	`, memType, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// scanMemories scans rows into Memory slice
func (k *KnowledgeStore) scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var projectID sql.NullString
		err := rows.Scan(&m.ID, &m.Type, &m.Content, &m.Context,
			&m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			continue
		}
		if projectID.Valid {
			m.ProjectID = projectID.String
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// UpdateMemory updates an existing memory's content and resets confidence
func (k *KnowledgeStore) UpdateMemory(m *Memory) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET content = ?, context = ?, confidence = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, m.Content, m.Context, m.Confidence, m.ID)
	return err
}

// ReinforceMemory increases confidence when memory is revalidated
func (k *KnowledgeStore) ReinforceMemory(id int64, delta float64) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = min(1.0, confidence + ?), updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, delta, id)
	return err
}

// DecayConfidence reduces confidence by rate per day since last update
func (k *KnowledgeStore) DecayConfidence(rate float64) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = max(0.0, confidence - (? * (julianday('now') - julianday(updated_at))))
		WHERE confidence > 0.1
	`, rate)
	return err
}

// PruneStale removes memories with confidence below threshold
func (k *KnowledgeStore) PruneStale(threshold float64) error {
	_, err := k.db.Exec(`DELETE FROM memories WHERE confidence < ?`, threshold)
	return err
}

// DeleteMemory removes a specific memory by ID
func (k *KnowledgeStore) DeleteMemory(id int64) error {
	_, err := k.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// GetRecentMemories returns the most recently updated memories
func (k *KnowledgeStore) GetRecentMemories(limit int) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE confidence > 0.1
		ORDER BY updated_at DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// CountByType returns the count of memories for each type
func (k *KnowledgeStore) CountByType(projectID string) (map[MemoryType]int, error) {
	rows, err := k.db.Query(`
		SELECT type, COUNT(*) as count
		FROM memories
		WHERE (project_id = ? OR project_id IS NULL OR project_id = '')
		AND confidence > 0.1
		GROUP BY type
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	counts := make(map[MemoryType]int)
	for rows.Next() {
		var memType string
		var count int
		if err := rows.Scan(&memType, &count); err != nil {
			continue
		}
		counts[MemoryType(memType)] = count
	}
	return counts, rows.Err()
}

// conceptPatterns defines common concepts to extract from memory content
var conceptPatterns = []string{
	"auth", "api", "database", "testing", "config",
	"webhook", "oauth", "jwt", "session", "cache",
	"error", "logging", "metrics", "deploy", "ci",
	"migration", "schema", "query", "index", "model",
	"handler", "middleware", "router", "endpoint", "service",
	"worker", "queue", "async", "sync", "event",
	"security", "encryption", "token", "permission", "role",
}

// extractConcepts extracts concept keywords from text content
func extractConcepts(content string) []string {
	lower := strings.ToLower(content)
	var found []string
	seen := make(map[string]bool)

	for _, p := range conceptPatterns {
		if strings.Contains(lower, p) && !seen[p] {
			found = append(found, p)
			seen[p] = true
		}
	}
	return found
}

// getOrCreateConcept retrieves an existing concept by name or creates a new one
func (k *KnowledgeStore) getOrCreateConcept(name string) (int64, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	// Try to get existing concept
	var id int64
	err := k.db.QueryRow("SELECT id FROM concepts WHERE name = ?", name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Create new concept
	result, err := k.db.Exec("INSERT INTO concepts (name) VALUES (?)", name)
	if err != nil {
		// Handle race condition - another insert may have succeeded
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return k.getOrCreateConcept(name)
		}
		return 0, err
	}
	return result.LastInsertId()
}

// linkMemoryToConcept creates a link between a memory and a concept
func (k *KnowledgeStore) linkMemoryToConcept(memoryID, conceptID int64) error {
	_, err := k.db.Exec(`
		INSERT OR IGNORE INTO memory_concepts (memory_id, concept_id)
		VALUES (?, ?)
	`, memoryID, conceptID)
	return err
}

// IndexConcepts extracts and indexes concepts from memory content
func (k *KnowledgeStore) IndexConcepts(memoryID int64, content string) error {
	concepts := extractConcepts(content)

	for _, c := range concepts {
		conceptID, err := k.getOrCreateConcept(c)
		if err != nil {
			return err
		}
		if err := k.linkMemoryToConcept(memoryID, conceptID); err != nil {
			return err
		}
	}
	return nil
}

// GetConcept retrieves a concept by ID with its linked memory IDs
func (k *KnowledgeStore) GetConcept(id int64) (*Concept, error) {
	var concept Concept
	err := k.db.QueryRow("SELECT id, name FROM concepts WHERE id = ?", id).Scan(&concept.ID, &concept.Name)
	if err != nil {
		return nil, err
	}

	// Get linked memory IDs
	rows, err := k.db.Query("SELECT memory_id FROM memory_concepts WHERE concept_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var memID int64
		if err := rows.Scan(&memID); err != nil {
			continue
		}
		concept.Memories = append(concept.Memories, memID)
	}

	return &concept, rows.Err()
}

// GetConceptByName retrieves a concept by name with its linked memory IDs
func (k *KnowledgeStore) GetConceptByName(name string) (*Concept, error) {
	name = strings.ToLower(strings.TrimSpace(name))

	var concept Concept
	err := k.db.QueryRow("SELECT id, name FROM concepts WHERE name = ?", name).Scan(&concept.ID, &concept.Name)
	if err != nil {
		return nil, err
	}

	// Get linked memory IDs
	rows, err := k.db.Query("SELECT memory_id FROM memory_concepts WHERE concept_id = ?", concept.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var memID int64
		if err := rows.Scan(&memID); err != nil {
			continue
		}
		concept.Memories = append(concept.Memories, memID)
	}

	return &concept, rows.Err()
}

// findRelatedConcepts finds concepts that match or are related to the query
func (k *KnowledgeStore) findRelatedConcepts(query string) ([]*Concept, error) {
	queryLower := strings.ToLower(query)

	// Find concepts whose names appear in the query or match common patterns
	rows, err := k.db.Query(`
		SELECT c.id, c.name
		FROM concepts c
		WHERE ? LIKE '%' || c.name || '%'
		   OR c.name LIKE '%' || ? || '%'
		LIMIT 10
	`, queryLower, queryLower)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var concepts []*Concept
	for rows.Next() {
		var c Concept
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			continue
		}

		// Get linked memory IDs for each concept
		memRows, err := k.db.Query("SELECT memory_id FROM memory_concepts WHERE concept_id = ?", c.ID)
		if err != nil {
			continue
		}
		for memRows.Next() {
			var memID int64
			if err := memRows.Scan(&memID); err != nil {
				continue
			}
			c.Memories = append(c.Memories, memID)
		}
		memRows.Close()

		concepts = append(concepts, &c)
	}

	return concepts, rows.Err()
}

// getMemoriesByConcept retrieves memories linked to a concept
func (k *KnowledgeStore) getMemoriesByConcept(conceptID int64) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT m.id, m.type, m.content, m.context, m.confidence, m.project_id, m.created_at, m.updated_at
		FROM memories m
		JOIN memory_concepts mc ON m.id = mc.memory_id
		WHERE mc.concept_id = ?
		AND m.confidence > 0.1
		ORDER BY m.confidence DESC
		LIMIT 20
	`, conceptID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// ScoredMemory wraps a memory with a relevance score
type ScoredMemory struct {
	Memory *Memory
	Score  float64
}

// scoreResults calculates relevance scores for query results
func (k *KnowledgeStore) scoreResults(result *GraphQueryResult) {
	queryLower := strings.ToLower(result.Query)
	queryWords := strings.Fields(queryLower)

	// Score direct matches
	scoredDirect := make([]ScoredMemory, 0, len(result.Memories))
	for _, m := range result.Memories {
		score := k.calculateMemoryScore(m, queryLower, queryWords)
		scoredDirect = append(scoredDirect, ScoredMemory{Memory: m, Score: score})
	}

	// Sort by score descending
	sort.Slice(scoredDirect, func(i, j int) bool {
		return scoredDirect[i].Score > scoredDirect[j].Score
	})

	// Rebuild memory list in scored order
	result.Memories = make([]*Memory, 0, len(scoredDirect))
	for _, sm := range scoredDirect {
		result.Memories = append(result.Memories, sm.Memory)
	}

	// Score related memories
	scoredRelated := make([]ScoredMemory, 0, len(result.RelatedMemories))
	for _, m := range result.RelatedMemories {
		score := k.calculateMemoryScore(m, queryLower, queryWords) * 0.8 // Discount related
		scoredRelated = append(scoredRelated, ScoredMemory{Memory: m, Score: score})
	}

	sort.Slice(scoredRelated, func(i, j int) bool {
		return scoredRelated[i].Score > scoredRelated[j].Score
	})

	result.RelatedMemories = make([]*Memory, 0, len(scoredRelated))
	for _, sm := range scoredRelated {
		result.RelatedMemories = append(result.RelatedMemories, sm.Memory)
	}
}

// calculateMemoryScore calculates a relevance score for a memory
func (k *KnowledgeStore) calculateMemoryScore(m *Memory, queryLower string, queryWords []string) float64 {
	score := m.Confidence // Base score from confidence

	contentLower := strings.ToLower(m.Content)
	contextLower := strings.ToLower(m.Context)

	// Exact phrase match bonus
	if strings.Contains(contentLower, queryLower) {
		score += 0.3
	}
	if strings.Contains(contextLower, queryLower) {
		score += 0.2
	}

	// Word match bonus
	wordMatches := 0
	for _, word := range queryWords {
		if len(word) < 3 {
			continue
		}
		if strings.Contains(contentLower, word) {
			wordMatches++
		}
		if strings.Contains(contextLower, word) {
			wordMatches++
		}
	}
	if len(queryWords) > 0 {
		score += float64(wordMatches) / float64(len(queryWords)*2) * 0.2
	}

	// Type bonuses for certain queries
	if strings.Contains(queryLower, "error") || strings.Contains(queryLower, "bug") {
		if m.Type == MemoryTypePitfall || m.Type == MemoryTypeLearning {
			score += 0.1
		}
	}
	if strings.Contains(queryLower, "how") || strings.Contains(queryLower, "pattern") {
		if m.Type == MemoryTypePattern {
			score += 0.1
		}
	}

	return score
}

// QueryGraph searches across all knowledge with concept expansion
func (k *KnowledgeStore) QueryGraph(query string, projectID string) (*GraphQueryResult, error) {
	result := &GraphQueryResult{
		Query: query,
	}

	// 1. Direct content match
	memories, err := k.QueryByTopic(query, projectID)
	if err != nil {
		return nil, err
	}
	result.Memories = memories

	// 2. Concept expansion - find related concepts
	concepts, err := k.findRelatedConcepts(query)
	if err != nil {
		return nil, err
	}
	result.RelatedConcepts = concepts

	// 3. Surface memories from related concepts (deduplicated)
	seenIDs := make(map[int64]bool)
	for _, m := range result.Memories {
		seenIDs[m.ID] = true
	}

	for _, c := range concepts {
		related, err := k.getMemoriesByConcept(c.ID)
		if err != nil {
			continue
		}
		for _, m := range related {
			// Filter by project and avoid duplicates
			if seenIDs[m.ID] {
				continue
			}
			if projectID != "" && m.ProjectID != "" && m.ProjectID != projectID {
				continue
			}
			seenIDs[m.ID] = true
			result.RelatedMemories = append(result.RelatedMemories, m)
		}
	}

	// 4. Score and rank by relevance
	k.scoreResults(result)

	return result, nil
}

// GetAllConcepts returns all concepts in the knowledge store
func (k *KnowledgeStore) GetAllConcepts() ([]*Concept, error) {
	rows, err := k.db.Query("SELECT id, name FROM concepts ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var concepts []*Concept
	for rows.Next() {
		var c Concept
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			continue
		}
		concepts = append(concepts, &c)
	}

	return concepts, rows.Err()
}

// UnlinkMemoryFromConcepts removes all concept links for a memory
func (k *KnowledgeStore) UnlinkMemoryFromConcepts(memoryID int64) error {
	_, err := k.db.Exec("DELETE FROM memory_concepts WHERE memory_id = ?", memoryID)
	return err
}

// ReindexMemory clears and re-indexes concepts for a memory
func (k *KnowledgeStore) ReindexMemory(memoryID int64) error {
	memory, err := k.GetMemory(memoryID)
	if err != nil {
		return err
	}

	if err := k.UnlinkMemoryFromConcepts(memoryID); err != nil {
		return err
	}

	return k.IndexConcepts(memoryID, memory.Content+" "+memory.Context)
}

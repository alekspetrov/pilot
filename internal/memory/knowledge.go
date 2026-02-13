package memory

import (
	"database/sql"
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

// KnowledgeStore manages experiential memories
type KnowledgeStore struct {
	db *sql.DB
}

// NewKnowledgeStore creates a knowledge store
func NewKnowledgeStore(db *sql.DB) *KnowledgeStore {
	return &KnowledgeStore{db: db}
}

// InitSchema creates the memories table
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

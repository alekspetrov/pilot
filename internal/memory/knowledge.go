package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

// KnowledgeStore manages experiential memories using SQLite
type KnowledgeStore struct {
	db *sql.DB
}

// NewKnowledgeStore creates a knowledge store with the given database connection
func NewKnowledgeStore(db *sql.DB) *KnowledgeStore {
	return &KnowledgeStore{db: db}
}

// InitSchema creates the memories table and indexes
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
		CREATE INDEX IF NOT EXISTS idx_memories_confidence ON memories(confidence DESC);
	`)
	return err
}

// AddMemory stores a new memory
func (k *KnowledgeStore) AddMemory(m *Memory) error {
	if m.Confidence == 0 {
		m.Confidence = 1.0
	}
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
	var context sql.NullString
	err := row.Scan(&m.ID, &m.Type, &m.Content, &context,
		&m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if projectID.Valid {
		m.ProjectID = projectID.String
	}
	if context.Valid {
		m.Context = context.String
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

// QueryByType retrieves memories of a specific type for a project
func (k *KnowledgeStore) QueryByType(memType MemoryType, projectID string) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE type = ?
		AND (project_id = ? OR project_id IS NULL OR project_id = '')
		AND confidence > 0.1
		ORDER BY confidence DESC, updated_at DESC
	`, memType, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// GetAllMemories retrieves all memories for a project
func (k *KnowledgeStore) GetAllMemories(projectID string) ([]*Memory, error) {
	rows, err := k.db.Query(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE (project_id = ? OR project_id IS NULL OR project_id = '')
		AND confidence > 0.1
		ORDER BY confidence DESC, updated_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// UpdateMemory updates an existing memory
func (k *KnowledgeStore) UpdateMemory(m *Memory) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET type = ?, content = ?, context = ?, confidence = ?, project_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, m.Type, m.Content, m.Context, m.Confidence, m.ProjectID, m.ID)
	return err
}

// DeleteMemory removes a memory by ID
func (k *KnowledgeStore) DeleteMemory(id int64) error {
	_, err := k.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// ReinforceMemory increases confidence when a memory is confirmed useful
func (k *KnowledgeStore) ReinforceMemory(id int64, delta float64) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = MIN(1.0, confidence + ?), updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, delta, id)
	return err
}

// DecayConfidence reduces confidence by rate per day since last update
func (k *KnowledgeStore) DecayConfidence(rate float64) error {
	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = MAX(0.0, confidence - (? * (julianday('now') - julianday(updated_at))))
		WHERE confidence > 0.1
	`, rate)
	return err
}

// PruneStale removes memories with confidence below threshold
func (k *KnowledgeStore) PruneStale(threshold float64) error {
	_, err := k.db.Exec(`DELETE FROM memories WHERE confidence < ?`, threshold)
	return err
}

// Count returns the total number of memories
func (k *KnowledgeStore) Count() (int, error) {
	var count int
	err := k.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&count)
	return count, err
}

// CountByType returns memory counts grouped by type
func (k *KnowledgeStore) CountByType() (map[MemoryType]int, error) {
	rows, err := k.db.Query(`
		SELECT type, COUNT(*) as count
		FROM memories
		GROUP BY type
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[MemoryType]int)
	for rows.Next() {
		var memType MemoryType
		var count int
		if err := rows.Scan(&memType, &count); err != nil {
			return nil, err
		}
		result[memType] = count
	}
	return result, nil
}

// SyncToFiles writes memories to .agent/memories/ for git tracking
func (k *KnowledgeStore) SyncToFiles(agentPath string) error {
	memoriesPath := filepath.Join(agentPath, "memories")
	if err := os.MkdirAll(memoriesPath, 0755); err != nil {
		return fmt.Errorf("failed to create memories directory: %w", err)
	}

	// Get all memories grouped by type
	types := []MemoryType{MemoryTypePattern, MemoryTypePitfall, MemoryTypeDecision, MemoryTypeLearning}

	for _, memType := range types {
		rows, err := k.db.Query(`
			SELECT id, type, content, context, confidence, project_id, created_at, updated_at
			FROM memories
			WHERE type = ? AND confidence > 0.1
			ORDER BY confidence DESC
		`, memType)
		if err != nil {
			return fmt.Errorf("failed to query memories: %w", err)
		}

		memories, err := k.scanMemories(rows)
		_ = rows.Close()
		if err != nil {
			return fmt.Errorf("failed to scan memories: %w", err)
		}

		if len(memories) == 0 {
			continue
		}

		// Write to file
		filename := filepath.Join(memoriesPath, string(memType)+"s.md")
		content := k.formatMemoriesAsMarkdown(memType, memories)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	return nil
}

// formatMemoriesAsMarkdown formats memories as a markdown document
func (k *KnowledgeStore) formatMemoriesAsMarkdown(memType MemoryType, memories []*Memory) string {
	var sb strings.Builder

	title := strings.Title(string(memType)) + "s"
	sb.WriteString("# " + title + "\n\n")
	sb.WriteString("_Auto-generated from knowledge store. Do not edit directly._\n\n")

	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("## %s (confidence: %.0f%%)\n\n", truncate(m.Content, 60), m.Confidence*100))
		sb.WriteString(m.Content + "\n\n")
		if m.Context != "" {
			sb.WriteString(fmt.Sprintf("**Context:** %s\n\n", m.Context))
		}
		if m.ProjectID != "" {
			sb.WriteString(fmt.Sprintf("**Project:** %s\n\n", m.ProjectID))
		}
		sb.WriteString("---\n\n")
	}

	return sb.String()
}

// scanMemories scans rows into Memory slice
func (k *KnowledgeStore) scanMemories(rows *sql.Rows) ([]*Memory, error) {
	var memories []*Memory
	for rows.Next() {
		m := &Memory{}
		var projectID sql.NullString
		var context sql.NullString
		err := rows.Scan(&m.ID, &m.Type, &m.Content, &context,
			&m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if projectID.Valid {
			m.ProjectID = projectID.String
		}
		if context.Valid {
			m.Context = context.String
		}
		memories = append(memories, m)
	}
	return memories, nil
}

// truncate truncates a string to the given length
func truncate(s string, length int) string {
	if len(s) <= length {
		return s
	}
	return s[:length-3] + "..."
}

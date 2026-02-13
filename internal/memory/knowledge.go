package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
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
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create memories table: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_memories_type ON memories(type)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_id)`,
		`CREATE INDEX IF NOT EXISTS idx_memories_confidence ON memories(confidence DESC)`,
	}

	for _, idx := range indexes {
		if _, err := k.db.Exec(idx); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// AddMemory stores a new memory
func (k *KnowledgeStore) AddMemory(m *Memory) error {
	if m.Content == "" {
		return fmt.Errorf("memory content is required")
	}
	if m.Type == "" {
		return fmt.Errorf("memory type is required")
	}

	// Default confidence if not set
	if m.Confidence == 0 {
		m.Confidence = 1.0
	}

	result, err := k.db.Exec(`
		INSERT INTO memories (type, content, context, confidence, project_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`, m.Type, m.Content, m.Context, m.Confidence, m.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to insert memory: %w", err)
	}

	m.ID, _ = result.LastInsertId()
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return nil
}

// GetMemory retrieves a memory by ID
func (k *KnowledgeStore) GetMemory(id int64) (*Memory, error) {
	row := k.db.QueryRow(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE id = ?
	`, id)

	m := &Memory{}
	var projectID sql.NullString
	var context sql.NullString
	err := row.Scan(&m.ID, &m.Type, &m.Content, &context, &m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
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

// UpdateMemory updates an existing memory
func (k *KnowledgeStore) UpdateMemory(m *Memory) error {
	if m.ID == 0 {
		return fmt.Errorf("memory ID is required for update")
	}

	_, err := k.db.Exec(`
		UPDATE memories
		SET type = ?, content = ?, context = ?, confidence = ?, project_id = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, m.Type, m.Content, m.Context, m.Confidence, m.ProjectID, m.ID)
	if err != nil {
		return fmt.Errorf("failed to update memory: %w", err)
	}

	m.UpdatedAt = time.Now()
	return nil
}

// DeleteMemory removes a memory by ID
func (k *KnowledgeStore) DeleteMemory(id int64) error {
	_, err := k.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	return err
}

// QueryByTopic searches memories by content/context
func (k *KnowledgeStore) QueryByTopic(topic string, projectID string) ([]*Memory, error) {
	query := `
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE (content LIKE ? OR context LIKE ?)
		AND confidence > 0.1
	`
	args := []interface{}{"%" + topic + "%", "%" + topic + "%"}

	if projectID != "" {
		query += ` AND (project_id = ? OR project_id IS NULL OR project_id = '')`
		args = append(args, projectID)
	}

	query += ` ORDER BY confidence DESC, updated_at DESC LIMIT 10`

	rows, err := k.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query memories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// QueryByType retrieves memories by type
func (k *KnowledgeStore) QueryByType(memType MemoryType, projectID string) ([]*Memory, error) {
	query := `
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE type = ?
		AND confidence > 0.1
	`
	args := []interface{}{memType}

	if projectID != "" {
		query += ` AND (project_id = ? OR project_id IS NULL OR project_id = '')`
		args = append(args, projectID)
	}

	query += ` ORDER BY confidence DESC, updated_at DESC`

	rows, err := k.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query memories by type: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

// GetAllMemories retrieves all memories for a project (or global if projectID is empty)
func (k *KnowledgeStore) GetAllMemories(projectID string) ([]*Memory, error) {
	var rows *sql.Rows
	var err error

	if projectID != "" {
		rows, err = k.db.Query(`
			SELECT id, type, content, context, confidence, project_id, created_at, updated_at
			FROM memories
			WHERE (project_id = ? OR project_id IS NULL OR project_id = '')
			AND confidence > 0.1
			ORDER BY confidence DESC, updated_at DESC
		`, projectID)
	} else {
		rows, err = k.db.Query(`
			SELECT id, type, content, context, confidence, project_id, created_at, updated_at
			FROM memories
			WHERE confidence > 0.1
			ORDER BY confidence DESC, updated_at DESC
		`)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get memories: %w", err)
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
		var context sql.NullString
		err := rows.Scan(&m.ID, &m.Type, &m.Content, &context, &m.Confidence, &projectID, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			continue
		}
		if projectID.Valid {
			m.ProjectID = projectID.String
		}
		if context.Valid {
			m.Context = context.String
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// DecayConfidence reduces confidence by rate per day since last update
func (k *KnowledgeStore) DecayConfidence(rate float64) error {
	if rate <= 0 {
		return fmt.Errorf("decay rate must be positive")
	}

	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = MAX(0.0, confidence - (? * (julianday('now') - julianday(updated_at)))),
		    updated_at = CURRENT_TIMESTAMP
		WHERE confidence > 0.1
	`, rate)
	if err != nil {
		return fmt.Errorf("failed to decay confidence: %w", err)
	}

	return nil
}

// ReinforceMemory increases confidence when memory is validated
func (k *KnowledgeStore) ReinforceMemory(id int64, boost float64) error {
	if boost <= 0 {
		boost = 0.1 // default boost
	}

	_, err := k.db.Exec(`
		UPDATE memories
		SET confidence = MIN(1.0, confidence + ?),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, boost, id)
	if err != nil {
		return fmt.Errorf("failed to reinforce memory: %w", err)
	}

	return nil
}

// PruneStale removes memories with confidence below threshold
func (k *KnowledgeStore) PruneStale(threshold float64) (int64, error) {
	if threshold <= 0 {
		threshold = 0.1 // default threshold
	}

	result, err := k.db.Exec(`DELETE FROM memories WHERE confidence < ?`, threshold)
	if err != nil {
		return 0, fmt.Errorf("failed to prune stale memories: %w", err)
	}

	return result.RowsAffected()
}

// GetStats returns statistics about stored memories
func (k *KnowledgeStore) GetStats() (*MemoryStats, error) {
	var stats MemoryStats

	row := k.db.QueryRow(`
		SELECT
			COUNT(*) as total,
			COALESCE(SUM(CASE WHEN type = 'pattern' THEN 1 ELSE 0 END), 0) as patterns,
			COALESCE(SUM(CASE WHEN type = 'pitfall' THEN 1 ELSE 0 END), 0) as pitfalls,
			COALESCE(SUM(CASE WHEN type = 'decision' THEN 1 ELSE 0 END), 0) as decisions,
			COALESCE(SUM(CASE WHEN type = 'learning' THEN 1 ELSE 0 END), 0) as learnings,
			COALESCE(AVG(confidence), 0) as avg_confidence,
			COUNT(DISTINCT project_id) as project_count
		FROM memories
		WHERE confidence > 0.1
	`)

	if err := row.Scan(
		&stats.Total,
		&stats.Patterns,
		&stats.Pitfalls,
		&stats.Decisions,
		&stats.Learnings,
		&stats.AvgConfidence,
		&stats.ProjectCount,
	); err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}

	return &stats, nil
}

// MemoryStats holds aggregate statistics about memories
type MemoryStats struct {
	Total         int
	Patterns      int
	Pitfalls      int
	Decisions     int
	Learnings     int
	AvgConfidence float64
	ProjectCount  int
}

// MemoryFile represents a memory as a markdown file for git tracking
type MemoryFile struct {
	Type       MemoryType `yaml:"type"`
	Content    string     `yaml:"content"`
	Context    string     `yaml:"context,omitempty"`
	Confidence float64    `yaml:"confidence"`
	CreatedAt  time.Time  `yaml:"created_at"`
	UpdatedAt  time.Time  `yaml:"updated_at"`
}

// SyncToFiles writes memories to .agent/memories/ for git tracking
func (k *KnowledgeStore) SyncToFiles(agentPath string) error {
	memoriesDir := filepath.Join(agentPath, "memories")
	if err := os.MkdirAll(memoriesDir, 0755); err != nil {
		return fmt.Errorf("failed to create memories directory: %w", err)
	}

	memories, err := k.GetAllMemories("")
	if err != nil {
		return fmt.Errorf("failed to get memories: %w", err)
	}

	// Group memories by type
	byType := make(map[MemoryType][]*Memory)
	for _, m := range memories {
		byType[m.Type] = append(byType[m.Type], m)
	}

	// Write each type to its own file
	for memType, mems := range byType {
		filename := filepath.Join(memoriesDir, string(memType)+".md")
		if err := k.writeMemoryFile(filename, memType, mems); err != nil {
			return fmt.Errorf("failed to write %s memories: %w", memType, err)
		}
	}

	return nil
}

// writeMemoryFile writes memories to a markdown file
func (k *KnowledgeStore) writeMemoryFile(filename string, memType MemoryType, memories []*Memory) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	// Write header
	title := strings.Title(string(memType)) + "s"
	fmt.Fprintf(f, "# %s\n\n", title)
	fmt.Fprintf(f, "_Auto-generated from knowledge store. Do not edit directly._\n\n")

	// Write each memory
	for _, m := range memories {
		fmt.Fprintf(f, "## %s\n\n", truncate(m.Content, 50))
		fmt.Fprintf(f, "%s\n\n", m.Content)

		if m.Context != "" {
			fmt.Fprintf(f, "**Context:** %s\n\n", m.Context)
		}

		fmt.Fprintf(f, "**Confidence:** %.0f%%\n", m.Confidence*100)
		fmt.Fprintf(f, "**Updated:** %s\n\n", m.UpdatedAt.Format("2006-01-02"))
		fmt.Fprintf(f, "---\n\n")
	}

	return nil
}

// LoadFromFiles loads memories from .agent/memories/ files
func (k *KnowledgeStore) LoadFromFiles(agentPath string, projectID string) error {
	memoriesDir := filepath.Join(agentPath, "memories")

	// Check if directory exists
	if _, err := os.Stat(memoriesDir); os.IsNotExist(err) {
		return nil // No memories directory, nothing to load
	}

	// Load each memory type file
	for _, memType := range []MemoryType{MemoryTypePattern, MemoryTypePitfall, MemoryTypeDecision, MemoryTypeLearning} {
		filename := filepath.Join(memoriesDir, string(memType)+".yaml")
		if err := k.loadMemoryYAML(filename, projectID); err != nil {
			// Log but don't fail on individual file errors
			continue
		}
	}

	return nil
}

// loadMemoryYAML loads memories from a YAML file
func (k *KnowledgeStore) loadMemoryYAML(filename string, projectID string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	var files []MemoryFile
	if err := yaml.Unmarshal(data, &files); err != nil {
		return fmt.Errorf("failed to parse %s: %w", filename, err)
	}

	for _, mf := range files {
		// Check if memory already exists (by content match)
		existing, _ := k.QueryByTopic(mf.Content, projectID)
		if len(existing) > 0 {
			// Memory already exists, reinforce it
			_ = k.ReinforceMemory(existing[0].ID, 0.05)
			continue
		}

		// Add new memory
		m := &Memory{
			Type:       mf.Type,
			Content:    mf.Content,
			Context:    mf.Context,
			Confidence: mf.Confidence,
			ProjectID:  projectID,
		}
		if err := k.AddMemory(m); err != nil {
			continue
		}
	}

	return nil
}

// truncate truncates a string to maxLen characters
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// FindSimilar finds memories similar to the given content
func (k *KnowledgeStore) FindSimilar(content string, projectID string, limit int) ([]*Memory, error) {
	if limit <= 0 {
		limit = 5
	}

	// Extract keywords for matching
	words := strings.Fields(strings.ToLower(content))
	if len(words) == 0 {
		return nil, nil
	}

	// Build LIKE conditions for each word
	var conditions []string
	var args []interface{}
	for _, word := range words {
		if len(word) > 3 { // Only use words longer than 3 chars
			conditions = append(conditions, "(content LIKE ? OR context LIKE ?)")
			args = append(args, "%"+word+"%", "%"+word+"%")
		}
	}

	if len(conditions) == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT id, type, content, context, confidence, project_id, created_at, updated_at
		FROM memories
		WHERE (%s)
		AND confidence > 0.1
	`, strings.Join(conditions, " OR "))

	if projectID != "" {
		query += ` AND (project_id = ? OR project_id IS NULL OR project_id = '')`
		args = append(args, projectID)
	}

	query += fmt.Sprintf(` ORDER BY confidence DESC LIMIT %d`, limit)

	rows, err := k.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to find similar memories: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return k.scanMemories(rows)
}

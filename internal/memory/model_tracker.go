package memory

import (
	"database/sql"
	"fmt"
	"time"
)

// ModelOutcomeTracker tracks model success/failure outcomes per task type
// to inform escalation decisions (e.g., Haiku → Sonnet → Opus).
type ModelOutcomeTracker struct {
	store             *Store
	failureThreshold  float64
	recentWindowSize  int
}

// ModelOutcomeTrackerOption configures a ModelOutcomeTracker.
type ModelOutcomeTrackerOption func(*ModelOutcomeTracker)

// WithFailureThreshold sets the failure rate threshold for escalation (default 0.3).
func WithFailureThreshold(t float64) ModelOutcomeTrackerOption {
	return func(m *ModelOutcomeTracker) {
		m.failureThreshold = t
	}
}

// WithRecentWindowSize sets how many recent outcomes to consider (default 10).
func WithRecentWindowSize(n int) ModelOutcomeTrackerOption {
	return func(m *ModelOutcomeTracker) {
		m.recentWindowSize = n
	}
}

// NewModelOutcomeTracker creates a tracker backed by the given Store.
func NewModelOutcomeTracker(store *Store, opts ...ModelOutcomeTrackerOption) *ModelOutcomeTracker {
	t := &ModelOutcomeTracker{
		store:            store,
		failureThreshold: 0.3,
		recentWindowSize: 10,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// RecordOutcome records a model execution outcome.
func (t *ModelOutcomeTracker) RecordOutcome(taskType, model, outcome string, tokens int, duration time.Duration) error {
	return t.store.withRetry("record_model_outcome", func() error {
		_, err := t.store.db.Exec(
			`INSERT INTO model_outcomes (task_type, model, outcome, tokens_used, duration_ms) VALUES (?, ?, ?, ?, ?)`,
			taskType, model, outcome, tokens, duration.Milliseconds(),
		)
		return err
	})
}

// GetFailureRate returns the failure rate for a task type and model
// over the most recent outcomes (window size configurable, default 10).
// Returns 0.0 if no outcomes exist.
func (t *ModelOutcomeTracker) GetFailureRate(taskType, model string) float64 {
	rows, err := t.store.db.Query(
		`SELECT outcome FROM model_outcomes WHERE task_type = ? AND model = ? ORDER BY id DESC LIMIT ?`,
		taskType, model, t.recentWindowSize,
	)
	if err != nil {
		return 0.0
	}
	defer rows.Close()

	var total, failures int
	for rows.Next() {
		var outcome string
		if err := rows.Scan(&outcome); err != nil {
			continue
		}
		total++
		if outcome == "failure" || outcome == "error" || outcome == "killed" {
			failures++
		}
	}

	if total == 0 {
		return 0.0
	}
	return float64(failures) / float64(total)
}

// escalationPath defines the model upgrade order.
var escalationPath = []string{
	"claude-haiku-4-5",
	"claude-sonnet-4-6",
	"claude-opus-4-6",
}

// ShouldEscalate checks if the failure rate for the given task type and model
// exceeds the threshold. Returns true with the suggested next model if escalation
// is warranted. Returns false if the model is already at the top of the escalation
// path or the failure rate is acceptable.
func (t *ModelOutcomeTracker) ShouldEscalate(taskType, model string) (bool, string) {
	rate := t.GetFailureRate(taskType, model)
	if rate <= t.failureThreshold {
		return false, ""
	}

	// Find current model in escalation path
	for i, m := range escalationPath {
		if m == model && i+1 < len(escalationPath) {
			return true, escalationPath[i+1]
		}
	}

	return false, ""
}

// GetOutcomeStats returns aggregate stats for a task type and model.
func (t *ModelOutcomeTracker) GetOutcomeStats(taskType, model string) (total int, failures int, avgTokens float64, err error) {
	row := t.store.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN outcome IN ('failure','error','killed') THEN 1 ELSE 0 END), 0), COALESCE(AVG(tokens_used), 0)
		 FROM (SELECT outcome, tokens_used FROM model_outcomes WHERE task_type = ? AND model = ? ORDER BY id DESC LIMIT ?)`,
		taskType, model, t.recentWindowSize,
	)
	err = row.Scan(&total, &failures, &avgTokens)
	if err == sql.ErrNoRows {
		return 0, 0, 0, nil
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("query outcome stats: %w", err)
	}
	return total, failures, avgTokens, nil
}

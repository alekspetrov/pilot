package autopilot

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// StateStore persists autopilot state to SQLite for crash recovery.
// It stores PR lifecycle state and processed issue tracking so that
// autopilot can resume from the correct stage after a restart.
type StateStore struct {
	db *sql.DB
}

// NewStateStore creates a StateStore using an existing *sql.DB connection.
// It runs migrations to create the required tables if they don't exist.
func NewStateStore(db *sql.DB) (*StateStore, error) {
	s := &StateStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("autopilot state store migration failed: %w", err)
	}
	return s, nil
}

// NewStateStoreFromPath creates a StateStore by opening a new SQLite connection.
// Used primarily for testing with in-memory databases (path = ":memory:").
func NewStateStoreFromPath(path string) (*StateStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("failed to set database pragmas: %w", err)
	}
	return NewStateStore(db)
}

func (s *StateStore) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS autopilot_pr_state (
			pr_number INTEGER PRIMARY KEY,
			pr_url TEXT NOT NULL,
			issue_number INTEGER DEFAULT 0,
			branch_name TEXT NOT NULL DEFAULT '',
			head_sha TEXT DEFAULT '',
			stage TEXT NOT NULL,
			ci_status TEXT NOT NULL DEFAULT 'pending',
			last_checked DATETIME,
			ci_wait_started_at DATETIME,
			merge_attempts INTEGER DEFAULT 0,
			error TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			release_version TEXT DEFAULT '',
			release_bump_type TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS autopilot_processed (
			issue_number INTEGER PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS autopilot_metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS autopilot_pr_failures (
			pr_number INTEGER PRIMARY KEY,
			failure_count INTEGER NOT NULL DEFAULT 0,
			last_failure_time DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// GH-1351: Linear processed issues table (uses string IDs unlike GitHub's integer IDs)
		`CREATE TABLE IF NOT EXISTS linear_processed (
			issue_id TEXT PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1356: GitLab processed issues table (uses integer IDs like GitHub)
		`CREATE TABLE IF NOT EXISTS gitlab_processed (
			issue_number INTEGER PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1356: Jira processed issues table (uses string keys)
		`CREATE TABLE IF NOT EXISTS jira_processed (
			issue_key TEXT PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1356: Asana processed tasks table (uses string GIDs)
		`CREATE TABLE IF NOT EXISTS asana_processed (
			task_gid TEXT PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1356: Azure DevOps processed work items table (uses integer IDs)
		`CREATE TABLE IF NOT EXISTS azuredevops_processed (
			work_item_id INTEGER PRIMARY KEY,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1829: Plane.so processed issues table (uses string IDs)
		`CREATE TABLE IF NOT EXISTS plane_processed (
			issue_id TEXT PRIMARY KEY,
			processed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT ''
		)`,
		// GH-1844: Unified adapter_processed table — all adapters share one table.
		// Old per-adapter tables are kept for backward compatibility but data is
		// migrated into this table on startup.
		`CREATE TABLE IF NOT EXISTS adapter_processed (
			adapter TEXT NOT NULL,
			issue_id TEXT NOT NULL,
			processed_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			result TEXT DEFAULT '',
			PRIMARY KEY (adapter, issue_id)
		)`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			// Ignore "duplicate column" errors from ALTER TABLE migrations
			if strings.Contains(err.Error(), "duplicate column") {
				continue
			}
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	// GH-1844: Idempotent data migration from old per-adapter tables into unified table.
	// Uses INSERT OR IGNORE so it's safe to re-run on every startup.
	dataMigrations := []string{
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'github', CAST(issue_number AS TEXT), processed_at, result FROM autopilot_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'linear', issue_id, processed_at, result FROM linear_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'gitlab', CAST(issue_number AS TEXT), processed_at, result FROM gitlab_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'jira', issue_key, processed_at, result FROM jira_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'asana', task_gid, processed_at, result FROM asana_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'azuredevops', CAST(work_item_id AS TEXT), processed_at, result FROM azuredevops_processed`,
		`INSERT OR IGNORE INTO adapter_processed (adapter, issue_id, processed_at, result)
			SELECT 'plane', issue_id, processed_at, result FROM plane_processed`,
	}
	for _, dm := range dataMigrations {
		if _, err := s.db.Exec(dm); err != nil {
			return fmt.Errorf("data migration failed: %w", err)
		}
	}

	return nil
}

// MarkAdapterProcessed records that an adapter issue has been processed.
func (s *StateStore) MarkAdapterProcessed(adapter, issueID, result string) error {
	_, err := s.db.Exec(`
		INSERT INTO adapter_processed (adapter, issue_id, processed_at, result)
		VALUES (?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(adapter, issue_id) DO UPDATE SET
			processed_at = CURRENT_TIMESTAMP,
			result = excluded.result
	`, adapter, issueID, result)
	return err
}

// UnmarkAdapterProcessed removes an adapter issue from the processed table.
func (s *StateStore) UnmarkAdapterProcessed(adapter, issueID string) error {
	_, err := s.db.Exec(`DELETE FROM adapter_processed WHERE adapter = ? AND issue_id = ?`, adapter, issueID)
	return err
}

// IsAdapterProcessed checks if an adapter issue has been previously processed.
func (s *StateStore) IsAdapterProcessed(adapter, issueID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM adapter_processed WHERE adapter = ? AND issue_id = ?`, adapter, issueID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// LoadAdapterProcessed returns a map of all processed issue IDs for the given adapter.
func (s *StateStore) LoadAdapterProcessed(adapter string) (map[string]bool, error) {
	rows, err := s.db.Query(`SELECT issue_id FROM adapter_processed WHERE adapter = ?`, adapter)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	processed := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		processed[id] = true
	}
	return processed, nil
}

// PurgeOldAdapterProcessed removes adapter processed records older than the given duration.
func (s *StateStore) PurgeOldAdapterProcessed(adapter string, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`DELETE FROM adapter_processed WHERE adapter = ? AND processed_at < ?`, adapter, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// toIntMap converts a map[string]bool (with string-encoded integer keys) to map[int]bool.
func toIntMap(m map[string]bool) (map[int]bool, error) {
	out := make(map[int]bool, len(m))
	for k, v := range m {
		n, err := strconv.Atoi(k)
		if err != nil {
			return nil, fmt.Errorf("invalid integer key %q: %w", k, err)
		}
		out[n] = v
	}
	return out, nil
}

// SavePRState persists a PR state to the database (upsert).
func (s *StateStore) SavePRState(pr *PRState) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_pr_state (
			pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at, updated_at,
			release_version, release_bump_type
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?, ?)
		ON CONFLICT(pr_number) DO UPDATE SET
			pr_url = excluded.pr_url,
			issue_number = excluded.issue_number,
			branch_name = excluded.branch_name,
			head_sha = excluded.head_sha,
			stage = excluded.stage,
			ci_status = excluded.ci_status,
			last_checked = excluded.last_checked,
			ci_wait_started_at = excluded.ci_wait_started_at,
			merge_attempts = excluded.merge_attempts,
			error = excluded.error,
			updated_at = CURRENT_TIMESTAMP,
			release_version = excluded.release_version,
			release_bump_type = excluded.release_bump_type
	`,
		pr.PRNumber, pr.PRURL, pr.IssueNumber, pr.BranchName, pr.HeadSHA,
		string(pr.Stage), string(pr.CIStatus),
		nullTime(pr.LastChecked), nullTime(pr.CIWaitStartedAt),
		pr.MergeAttempts, pr.Error, nullTime(pr.CreatedAt),
		pr.ReleaseVersion, string(pr.ReleaseBumpType),
	)
	return err
}

// GetPRState retrieves a single PR state by number.
// Returns nil, nil if not found.
func (s *StateStore) GetPRState(prNumber int) (*PRState, error) {
	row := s.db.QueryRow(`
		SELECT pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at,
			release_version, release_bump_type
		FROM autopilot_pr_state WHERE pr_number = ?
	`, prNumber)

	pr, err := scanPRState(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return pr, nil
}

// LoadAllPRStates retrieves all persisted PR states.
func (s *StateStore) LoadAllPRStates() ([]*PRState, error) {
	rows, err := s.db.Query(`
		SELECT pr_number, pr_url, issue_number, branch_name, head_sha,
			stage, ci_status, last_checked, ci_wait_started_at,
			merge_attempts, error, created_at,
			release_version, release_bump_type
		FROM autopilot_pr_state
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var states []*PRState
	for rows.Next() {
		var pr PRState
		var lastChecked, ciWaitStartedAt, createdAt sql.NullTime
		var stage, ciStatus, relBumpType string

		if err := rows.Scan(
			&pr.PRNumber, &pr.PRURL, &pr.IssueNumber, &pr.BranchName, &pr.HeadSHA,
			&stage, &ciStatus, &lastChecked, &ciWaitStartedAt,
			&pr.MergeAttempts, &pr.Error, &createdAt,
			&pr.ReleaseVersion, &relBumpType,
		); err != nil {
			return nil, err
		}

		pr.Stage = PRStage(stage)
		pr.CIStatus = CIStatus(ciStatus)
		pr.ReleaseBumpType = BumpType(relBumpType)
		if lastChecked.Valid {
			pr.LastChecked = lastChecked.Time
		}
		if ciWaitStartedAt.Valid {
			pr.CIWaitStartedAt = ciWaitStartedAt.Time
		}
		if createdAt.Valid {
			pr.CreatedAt = createdAt.Time
		}
		states = append(states, &pr)
	}
	return states, nil
}

// RemovePRState deletes a PR state record.
func (s *StateStore) RemovePRState(prNumber int) error {
	_, err := s.db.Exec(`DELETE FROM autopilot_pr_state WHERE pr_number = ?`, prNumber)
	return err
}

// MarkIssueProcessed records that a GitHub issue has been processed.
func (s *StateStore) MarkIssueProcessed(issueNumber int, result string) error {
	return s.MarkAdapterProcessed("github", strconv.Itoa(issueNumber), result)
}

// UnmarkIssueProcessed removes a GitHub issue from the processed table.
func (s *StateStore) UnmarkIssueProcessed(issueNumber int) error {
	return s.UnmarkAdapterProcessed("github", strconv.Itoa(issueNumber))
}

// IsIssueProcessed checks if a GitHub issue has been previously processed.
func (s *StateStore) IsIssueProcessed(issueNumber int) (bool, error) {
	return s.IsAdapterProcessed("github", strconv.Itoa(issueNumber))
}

// LoadProcessedIssues returns a map of all processed GitHub issue numbers.
func (s *StateStore) LoadProcessedIssues() (map[int]bool, error) {
	m, err := s.LoadAdapterProcessed("github")
	if err != nil {
		return nil, err
	}
	return toIntMap(m)
}

// MarkLinearIssueProcessed records that a Linear issue has been processed.
func (s *StateStore) MarkLinearIssueProcessed(issueID string, result string) error {
	return s.MarkAdapterProcessed("linear", issueID, result)
}

// UnmarkLinearIssueProcessed removes a Linear issue from the processed table.
func (s *StateStore) UnmarkLinearIssueProcessed(issueID string) error {
	return s.UnmarkAdapterProcessed("linear", issueID)
}

// IsLinearIssueProcessed checks if a Linear issue has been previously processed.
func (s *StateStore) IsLinearIssueProcessed(issueID string) (bool, error) {
	return s.IsAdapterProcessed("linear", issueID)
}

// LoadLinearProcessedIssues returns a map of all processed Linear issue IDs.
func (s *StateStore) LoadLinearProcessedIssues() (map[string]bool, error) {
	return s.LoadAdapterProcessed("linear")
}

// PurgeOldLinearProcessedIssues removes Linear processed issue records older than the given duration.
func (s *StateStore) PurgeOldLinearProcessedIssues(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("linear", olderThan)
}

// MarkGitLabIssueProcessed records that a GitLab issue has been processed.
func (s *StateStore) MarkGitLabIssueProcessed(issueNumber int, result string) error {
	return s.MarkAdapterProcessed("gitlab", strconv.Itoa(issueNumber), result)
}

// UnmarkGitLabIssueProcessed removes a GitLab issue from the processed table.
func (s *StateStore) UnmarkGitLabIssueProcessed(issueNumber int) error {
	return s.UnmarkAdapterProcessed("gitlab", strconv.Itoa(issueNumber))
}

// IsGitLabIssueProcessed checks if a GitLab issue has been previously processed.
func (s *StateStore) IsGitLabIssueProcessed(issueNumber int) (bool, error) {
	return s.IsAdapterProcessed("gitlab", strconv.Itoa(issueNumber))
}

// LoadGitLabProcessedIssues returns a map of all processed GitLab issue numbers.
func (s *StateStore) LoadGitLabProcessedIssues() (map[int]bool, error) {
	m, err := s.LoadAdapterProcessed("gitlab")
	if err != nil {
		return nil, err
	}
	return toIntMap(m)
}

// PurgeOldGitLabProcessedIssues removes GitLab processed issue records older than the given duration.
func (s *StateStore) PurgeOldGitLabProcessedIssues(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("gitlab", olderThan)
}

// MarkJiraIssueProcessed records that a Jira issue has been processed.
func (s *StateStore) MarkJiraIssueProcessed(issueKey string, result string) error {
	return s.MarkAdapterProcessed("jira", issueKey, result)
}

// UnmarkJiraIssueProcessed removes a Jira issue from the processed table.
func (s *StateStore) UnmarkJiraIssueProcessed(issueKey string) error {
	return s.UnmarkAdapterProcessed("jira", issueKey)
}

// IsJiraIssueProcessed checks if a Jira issue has been previously processed.
func (s *StateStore) IsJiraIssueProcessed(issueKey string) (bool, error) {
	return s.IsAdapterProcessed("jira", issueKey)
}

// LoadJiraProcessedIssues returns a map of all processed Jira issue keys.
func (s *StateStore) LoadJiraProcessedIssues() (map[string]bool, error) {
	return s.LoadAdapterProcessed("jira")
}

// PurgeOldJiraProcessedIssues removes Jira processed issue records older than the given duration.
func (s *StateStore) PurgeOldJiraProcessedIssues(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("jira", olderThan)
}

// MarkAsanaTaskProcessed records that an Asana task has been processed.
func (s *StateStore) MarkAsanaTaskProcessed(taskGID string, result string) error {
	return s.MarkAdapterProcessed("asana", taskGID, result)
}

// UnmarkAsanaTaskProcessed removes an Asana task from the processed table.
func (s *StateStore) UnmarkAsanaTaskProcessed(taskGID string) error {
	return s.UnmarkAdapterProcessed("asana", taskGID)
}

// IsAsanaTaskProcessed checks if an Asana task has been previously processed.
func (s *StateStore) IsAsanaTaskProcessed(taskGID string) (bool, error) {
	return s.IsAdapterProcessed("asana", taskGID)
}

// LoadAsanaProcessedTasks returns a map of all processed Asana task GIDs.
func (s *StateStore) LoadAsanaProcessedTasks() (map[string]bool, error) {
	return s.LoadAdapterProcessed("asana")
}

// PurgeOldAsanaProcessedTasks removes Asana processed task records older than the given duration.
func (s *StateStore) PurgeOldAsanaProcessedTasks(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("asana", olderThan)
}

// MarkAzureDevOpsWorkItemProcessed records that an Azure DevOps work item has been processed.
func (s *StateStore) MarkAzureDevOpsWorkItemProcessed(workItemID int, result string) error {
	return s.MarkAdapterProcessed("azuredevops", strconv.Itoa(workItemID), result)
}

// UnmarkAzureDevOpsWorkItemProcessed removes an Azure DevOps work item from the processed table.
func (s *StateStore) UnmarkAzureDevOpsWorkItemProcessed(workItemID int) error {
	return s.UnmarkAdapterProcessed("azuredevops", strconv.Itoa(workItemID))
}

// IsAzureDevOpsWorkItemProcessed checks if an Azure DevOps work item has been previously processed.
func (s *StateStore) IsAzureDevOpsWorkItemProcessed(workItemID int) (bool, error) {
	return s.IsAdapterProcessed("azuredevops", strconv.Itoa(workItemID))
}

// LoadAzureDevOpsProcessedWorkItems returns a map of all processed Azure DevOps work item IDs.
func (s *StateStore) LoadAzureDevOpsProcessedWorkItems() (map[int]bool, error) {
	m, err := s.LoadAdapterProcessed("azuredevops")
	if err != nil {
		return nil, err
	}
	return toIntMap(m)
}

// PurgeOldAzureDevOpsProcessedWorkItems removes Azure DevOps processed work item records older than the given duration.
func (s *StateStore) PurgeOldAzureDevOpsProcessedWorkItems(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("azuredevops", olderThan)
}

// MarkPlaneIssueProcessed records that a Plane.so issue has been processed.
func (s *StateStore) MarkPlaneIssueProcessed(issueID string, result string) error {
	return s.MarkAdapterProcessed("plane", issueID, result)
}

// UnmarkPlaneIssueProcessed removes a Plane.so issue from the processed table.
func (s *StateStore) UnmarkPlaneIssueProcessed(issueID string) error {
	return s.UnmarkAdapterProcessed("plane", issueID)
}

// IsPlaneIssueProcessed checks if a Plane.so issue has been previously processed.
func (s *StateStore) IsPlaneIssueProcessed(issueID string) (bool, error) {
	return s.IsAdapterProcessed("plane", issueID)
}

// LoadPlaneProcessedIssues returns a map of all processed Plane.so issue IDs.
func (s *StateStore) LoadPlaneProcessedIssues() (map[string]bool, error) {
	return s.LoadAdapterProcessed("plane")
}

// PurgeOldPlaneProcessedIssues removes Plane processed issue records older than the given duration.
func (s *StateStore) PurgeOldPlaneProcessedIssues(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("plane", olderThan)
}

// SaveMetadata stores a key-value pair in the metadata table.
func (s *StateStore) SaveMetadata(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_metadata (key, value, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			updated_at = CURRENT_TIMESTAMP
	`, key, value)
	return err
}

// GetMetadata retrieves a metadata value by key.
// Returns empty string if not found.
func (s *StateStore) GetMetadata(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM autopilot_metadata WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SavePRFailures persists the per-PR failure state.
func (s *StateStore) SavePRFailures(prNumber, failureCount int, lastFailureTime time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO autopilot_pr_failures (pr_number, failure_count, last_failure_time)
		VALUES (?, ?, ?)
		ON CONFLICT(pr_number) DO UPDATE SET
			failure_count = excluded.failure_count,
			last_failure_time = excluded.last_failure_time
	`, prNumber, failureCount, lastFailureTime)
	return err
}

// RemovePRFailures removes per-PR failure state.
func (s *StateStore) RemovePRFailures(prNumber int) error {
	_, err := s.db.Exec(`DELETE FROM autopilot_pr_failures WHERE pr_number = ?`, prNumber)
	return err
}

// LoadAllPRFailures loads all per-PR failure states.
func (s *StateStore) LoadAllPRFailures() (map[int]*prFailureState, error) {
	rows, err := s.db.Query(`
		SELECT pr_number, failure_count, last_failure_time
		FROM autopilot_pr_failures
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	failures := make(map[int]*prFailureState)
	for rows.Next() {
		var prNumber, failureCount int
		var lastFailureTime time.Time

		if err := rows.Scan(&prNumber, &failureCount, &lastFailureTime); err != nil {
			return nil, err
		}

		failures[prNumber] = &prFailureState{
			FailureCount:    failureCount,
			LastFailureTime: lastFailureTime,
		}
	}
	return failures, nil
}

// PurgeOldProcessedIssues removes GitHub processed issue records older than the given duration.
func (s *StateStore) PurgeOldProcessedIssues(olderThan time.Duration) (int64, error) {
	return s.PurgeOldAdapterProcessed("github", olderThan)
}

// PurgeTerminalPRStates removes PR states in terminal stages (failed, merged+removed).
// This is for housekeeping — active PRs are never purged.
func (s *StateStore) PurgeTerminalPRStates(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`
		DELETE FROM autopilot_pr_state
		WHERE stage IN ('failed') AND updated_at < ?
	`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// scanPRState scans a single row into a PRState.
func scanPRState(row *sql.Row) (*PRState, error) {
	var pr PRState
	var lastChecked, ciWaitStartedAt, createdAt sql.NullTime
	var stage, ciStatus, relBumpType string

	err := row.Scan(
		&pr.PRNumber, &pr.PRURL, &pr.IssueNumber, &pr.BranchName, &pr.HeadSHA,
		&stage, &ciStatus, &lastChecked, &ciWaitStartedAt,
		&pr.MergeAttempts, &pr.Error, &createdAt,
		&pr.ReleaseVersion, &relBumpType,
	)
	if err != nil {
		return nil, err
	}

	pr.Stage = PRStage(stage)
	pr.CIStatus = CIStatus(ciStatus)
	pr.ReleaseBumpType = BumpType(relBumpType)
	if lastChecked.Valid {
		pr.LastChecked = lastChecked.Time
	}
	if ciWaitStartedAt.Valid {
		pr.CIWaitStartedAt = ciWaitStartedAt.Time
	}
	if createdAt.Valid {
		pr.CreatedAt = createdAt.Time
	}
	return &pr, nil
}

// nullTime converts a time.Time to sql.NullTime, treating zero time as NULL.
func nullTime(t time.Time) sql.NullTime {
	if t.IsZero() {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

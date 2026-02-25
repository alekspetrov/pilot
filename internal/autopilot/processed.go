package autopilot

import "time"

// AdapterProcessedStore is the generic interface for tracking processed issues
// across all adapters. New adapters should use this interface instead of adding
// adapter-specific methods to StateStore.
//
// The adapter parameter is a string key identifying the adapter (e.g. "github",
// "linear", "gitlab", "jira", "asana", "azuredevops", "plane"). The issueID
// is always stored as TEXT — integer-based adapters convert via strconv.Itoa.
type AdapterProcessedStore interface {
	MarkAdapterProcessed(adapter, issueID, result string) error
	UnmarkAdapterProcessed(adapter, issueID string) error
	IsAdapterProcessed(adapter, issueID string) (bool, error)
	LoadAdapterProcessed(adapter string) (map[string]bool, error)
	PurgeOldAdapterProcessed(adapter string, olderThan time.Duration) (int64, error)
}

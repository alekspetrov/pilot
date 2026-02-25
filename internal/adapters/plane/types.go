// Package plane provides an adapter for Plane.so project management.
package plane

import "time"

// Config holds Plane adapter configuration
type Config struct {
	Enabled       bool     `yaml:"enabled"`
	BaseURL       string   `yaml:"base_url"`        // e.g., "https://api.plane.so" or self-hosted URL
	APIKey        string   `yaml:"api_key"`          // Plane API key
	WorkspaceSlug string   `yaml:"workspace_slug"`   // Workspace slug
	ProjectIDs    []string `yaml:"project_ids"`      // Filter by project UUIDs
	WebhookSecret string   `yaml:"webhook_secret"`   // For HMAC signature verification
	PilotLabel    string   `yaml:"pilot_label"`      // Label name that triggers Pilot (default: "pilot")

	// Polling configuration
	Polling *PollingConfig `yaml:"polling,omitempty"`
}

// PollingConfig holds polling configuration for Plane adapter
type PollingConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval,omitempty"`
}

// DefaultConfig returns default Plane configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:    false,
		BaseURL:    "https://api.plane.so",
		PilotLabel: "pilot",
	}
}

// WorkItem represents a Plane work item (issue)
type WorkItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description_html,omitempty"`
	StateID     string    `json:"state"`
	Priority    string    `json:"priority"` // "urgent", "high", "medium", "low", "none"
	Labels      []string  `json:"labels"`   // Array of label UUIDs
	ProjectID   string    `json:"project"`
	WorkspaceID string    `json:"workspace"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Sequence    int       `json:"sequence_id"`
}

// Label represents a Plane label
type Label struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// State represents a Plane project state
type State struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"` // "backlog", "unstarted", "started", "completed", "cancelled"
	Color string `json:"color,omitempty"`
}

// ListResponse wraps paginated API responses
type ListResponse[T any] struct {
	Results    []T  `json:"results"`
	TotalCount int  `json:"total_count"`
	NextPage   *int `json:"next_page_id,omitempty"`
}

// Priority levels matching Plane API
const (
	PriorityUrgent = "urgent"
	PriorityHigh   = "high"
	PriorityMedium = "medium"
	PriorityLow    = "low"
	PriorityNone   = "none"
)

// State groups (fixed in Plane)
const (
	StateGroupBacklog   = "backlog"
	StateGroupUnstarted = "unstarted"
	StateGroupStarted   = "started"
	StateGroupCompleted = "completed"
	StateGroupCancelled = "cancelled"
)

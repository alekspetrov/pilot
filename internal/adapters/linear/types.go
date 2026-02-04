package linear

import "time"

// Config holds Linear adapter configuration
type Config struct {
	Enabled    bool               `yaml:"enabled"`
	Workspaces []*WorkspaceConfig `yaml:"workspaces,omitempty"`

	// Legacy single-workspace fields (backward compatible)
	APIKey     string   `yaml:"api_key,omitempty"`
	TeamID     string   `yaml:"team_id,omitempty"`
	AutoAssign bool     `yaml:"auto_assign"`
	PilotLabel string   `yaml:"pilot_label,omitempty"`
	ProjectIDs []string `yaml:"project_ids,omitempty"` // Filter issues by project ID(s)

	// Polling configuration
	Polling *PollingConfig `yaml:"polling,omitempty"`
}

// PollingConfig holds Linear polling configuration
type PollingConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"`
}

// WorkspaceConfig holds configuration for a single Linear workspace
type WorkspaceConfig struct {
	Name       string   `yaml:"name"`
	APIKey     string   `yaml:"api_key"`
	TeamID     string   `yaml:"team_id"`
	PilotLabel string   `yaml:"pilot_label"`
	ProjectIDs []string `yaml:"project_ids,omitempty"`
	Projects   []string `yaml:"projects"` // Pilot project names
	AutoAssign bool     `yaml:"auto_assign"`

	// Polling configuration (inherits from parent if not set)
	Polling *PollingConfig `yaml:"polling,omitempty"`
}

// GetWorkspaces returns all configured workspaces.
// If workspaces array is set, returns it directly.
// Otherwise, converts legacy single-workspace config to a workspace slice for backward compatibility.
func (c *Config) GetWorkspaces() []*WorkspaceConfig {
	if len(c.Workspaces) > 0 {
		return c.Workspaces
	}

	// Legacy single-workspace mode
	if c.APIKey != "" {
		pilotLabel := c.PilotLabel
		if pilotLabel == "" {
			pilotLabel = "pilot"
		}
		return []*WorkspaceConfig{{
			Name:       "default",
			APIKey:     c.APIKey,
			TeamID:     c.TeamID,
			PilotLabel: pilotLabel,
			ProjectIDs: c.ProjectIDs,
			AutoAssign: c.AutoAssign,
		}}
	}

	return nil
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	workspaces := c.GetWorkspaces()
	if len(workspaces) == 0 {
		return nil // No workspaces configured
	}

	// Check for duplicate team IDs
	seenTeamIDs := make(map[string]string) // team_id -> workspace name
	for _, ws := range workspaces {
		if ws.TeamID == "" {
			continue
		}
		if existing, ok := seenTeamIDs[ws.TeamID]; ok {
			return &DuplicateTeamIDError{
				TeamID:     ws.TeamID,
				Workspace1: existing,
				Workspace2: ws.Name,
			}
		}
		seenTeamIDs[ws.TeamID] = ws.Name
	}

	return nil
}

// DuplicateTeamIDError is returned when two workspaces have the same team ID
type DuplicateTeamIDError struct {
	TeamID     string
	Workspace1 string
	Workspace2 string
}

func (e *DuplicateTeamIDError) Error() string {
	return "duplicate team_id '" + e.TeamID + "' in workspaces '" + e.Workspace1 + "' and '" + e.Workspace2 + "'"
}

// DefaultConfig returns default Linear configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:    false,
		PilotLabel: "pilot",
		AutoAssign: true,
		Polling: &PollingConfig{
			Enabled:  true,
			Interval: 30 * time.Second,
		},
	}
}

// GetPollingInterval returns the polling interval for a workspace
func (c *WorkspaceConfig) GetPollingInterval(parentConfig *Config) time.Duration {
	// Workspace-specific polling config takes precedence
	if c.Polling != nil && c.Polling.Interval > 0 {
		return c.Polling.Interval
	}
	// Fall back to parent config
	if parentConfig != nil && parentConfig.Polling != nil && parentConfig.Polling.Interval > 0 {
		return parentConfig.Polling.Interval
	}
	// Default to 30 seconds
	return 30 * time.Second
}

// IsPollingEnabled returns whether polling is enabled for a workspace
func (c *WorkspaceConfig) IsPollingEnabled(parentConfig *Config) bool {
	// Workspace-specific polling config takes precedence
	if c.Polling != nil {
		return c.Polling.Enabled
	}
	// Fall back to parent config
	if parentConfig != nil && parentConfig.Polling != nil {
		return parentConfig.Polling.Enabled
	}
	// Default to enabled
	return true
}

// Priority levels
const (
	PriorityNone   = 0
	PriorityUrgent = 1
	PriorityHigh   = 2
	PriorityMedium = 3
	PriorityLow    = 4
)

// PriorityName returns the human-readable priority name
func PriorityName(priority int) string {
	switch priority {
	case PriorityUrgent:
		return "Urgent"
	case PriorityHigh:
		return "High"
	case PriorityMedium:
		return "Medium"
	case PriorityLow:
		return "Low"
	default:
		return "No Priority"
	}
}

// StateType represents issue state types
type StateType string

const (
	StateTypeBacklog   StateType = "backlog"
	StateTypeUnstarted StateType = "unstarted"
	StateTypeStarted   StateType = "started"
	StateTypeCompleted StateType = "completed"
	StateTypeCanceled  StateType = "canceled"
)

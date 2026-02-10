package slack

// ProjectSource defines the interface for resolving project information from Slack messages
type ProjectSource interface {
	// GetProject returns project info for a given channel ID
	GetProject(channelID string) (*ProjectInfo, error)

	// ListProjects returns all configured projects
	ListProjects() []*ProjectInfo
}

// ProjectInfo holds project configuration for multi-project support
type ProjectInfo struct {
	// Name is the project identifier (e.g., "pilot", "backend")
	Name string `yaml:"name"`

	// ChannelID is the Slack channel ID associated with this project
	ChannelID string `yaml:"channel_id"`

	// Repository is the Git repository URL or path
	Repository string `yaml:"repository"`

	// WorkDir is the working directory for project execution
	WorkDir string `yaml:"work_dir"`

	// Branch is the default branch for this project (e.g., "main")
	Branch string `yaml:"branch"`

	// Labels are default labels to apply to issues created from this channel
	Labels []string `yaml:"labels"`

	// Enabled indicates if the project is active
	Enabled bool `yaml:"enabled"`
}

// DefaultProjectInfo returns a ProjectInfo with sensible defaults
func DefaultProjectInfo(name string) *ProjectInfo {
	return &ProjectInfo{
		Name:    name,
		Branch:  "main",
		Labels:  []string{"pilot"},
		Enabled: true,
	}
}

// StaticProjectSource implements ProjectSource with a static list of projects
type StaticProjectSource struct {
	projects   map[string]*ProjectInfo // keyed by channel ID
	allProjects []*ProjectInfo
}

// NewStaticProjectSource creates a project source from a list of project configs
func NewStaticProjectSource(projects []*ProjectInfo) *StaticProjectSource {
	source := &StaticProjectSource{
		projects:    make(map[string]*ProjectInfo),
		allProjects: projects,
	}
	for _, p := range projects {
		if p.ChannelID != "" {
			source.projects[p.ChannelID] = p
		}
	}
	return source
}

// GetProject returns project info for a given channel ID
func (s *StaticProjectSource) GetProject(channelID string) (*ProjectInfo, error) {
	if p, ok := s.projects[channelID]; ok {
		return p, nil
	}
	return nil, nil // No project found for this channel
}

// ListProjects returns all configured projects
func (s *StaticProjectSource) ListProjects() []*ProjectInfo {
	return s.allProjects
}

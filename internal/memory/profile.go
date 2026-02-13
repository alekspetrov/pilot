package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// UserProfile stores learned user preferences
type UserProfile struct {
	mu sync.RWMutex

	// Communication style
	Verbosity string `json:"verbosity"` // "concise" or "detailed"

	// Code preferences
	Frameworks   []string          `json:"frameworks"`    // ["gin", "gorm"]
	Conventions  map[string]string `json:"conventions"`   // {"indent": "tabs"}
	CodePatterns []string          `json:"code_patterns"` // ["early_returns"]

	// Corrections learned
	Corrections []Correction `json:"corrections"`
}

// Correction represents a learned correction
type Correction struct {
	Pattern    string `json:"pattern"`    // What triggered correction
	Correction string `json:"correction"` // What user said
	Count      int    `json:"count"`      // Times this came up
}

// ProfileManager handles profile loading/saving
type ProfileManager struct {
	globalPath  string // ~/.pilot/profile.json
	projectPath string // .agent/.user-profile.json
}

// NewProfileManager creates a profile manager
func NewProfileManager(globalPath, projectPath string) *ProfileManager {
	return &ProfileManager{
		globalPath:  globalPath,
		projectPath: projectPath,
	}
}

// Load loads profile with project overrides
func (pm *ProfileManager) Load() (*UserProfile, error) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// Load global defaults
	if data, err := os.ReadFile(pm.globalPath); err == nil {
		json.Unmarshal(data, profile)
	}

	// Apply project overrides
	if data, err := os.ReadFile(pm.projectPath); err == nil {
		var projectProfile UserProfile
		if json.Unmarshal(data, &projectProfile) == nil {
			pm.mergeProfiles(profile, &projectProfile)
		}
	}

	return profile, nil
}

// Save saves profile to both global and project paths
func (pm *ProfileManager) Save(profile *UserProfile, global bool) error {
	profile.mu.RLock()
	defer profile.mu.RUnlock()

	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}

	path := pm.projectPath
	if global {
		path = pm.globalPath
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

// RecordCorrection learns from a user correction
func (profile *UserProfile) RecordCorrection(pattern, correction string) {
	profile.mu.Lock()
	defer profile.mu.Unlock()

	// Check if we've seen this before
	for i := range profile.Corrections {
		if profile.Corrections[i].Pattern == pattern {
			profile.Corrections[i].Count++
			profile.Corrections[i].Correction = correction
			return
		}
	}

	// New correction
	profile.Corrections = append(profile.Corrections, Correction{
		Pattern:    pattern,
		Correction: correction,
		Count:      1,
	})
}

// SetPreference sets a convention preference
func (profile *UserProfile) SetPreference(key, value string) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	profile.Conventions[key] = value
}

// GetPreference gets a convention preference
func (profile *UserProfile) GetPreference(key string) string {
	profile.mu.RLock()
	defer profile.mu.RUnlock()
	return profile.Conventions[key]
}

// mergeProfiles applies project overrides to base profile
func (pm *ProfileManager) mergeProfiles(base, override *UserProfile) {
	if override.Verbosity != "" {
		base.Verbosity = override.Verbosity
	}
	base.Frameworks = append(base.Frameworks, override.Frameworks...)
	for k, v := range override.Conventions {
		base.Conventions[k] = v
	}
	base.CodePatterns = append(base.CodePatterns, override.CodePatterns...)
}

package memory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// UserProfile stores learned user preferences across sessions.
// It tracks communication style, code preferences, and corrections.
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

// Correction represents a learned correction from user feedback.
type Correction struct {
	Pattern    string `json:"pattern"`    // What triggered correction
	Correction string `json:"correction"` // What user said
	Count      int    `json:"count"`      // Times this came up
}

// ProfileManager handles profile loading and saving.
// It supports both global (~/.pilot/profile.json) and
// project-specific (.agent/.user-profile.json) profiles.
type ProfileManager struct {
	globalPath  string // ~/.pilot/profile.json
	projectPath string // .agent/.user-profile.json
}

// NewProfileManager creates a profile manager with the specified paths.
func NewProfileManager(globalPath, projectPath string) *ProfileManager {
	return &ProfileManager{
		globalPath:  globalPath,
		projectPath: projectPath,
	}
}

// Load loads profile with project overrides.
// Global defaults are loaded first, then project-specific settings are merged.
func (pm *ProfileManager) Load() (*UserProfile, error) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// Load global defaults
	if data, err := os.ReadFile(pm.globalPath); err == nil {
		_ = json.Unmarshal(data, profile)
	}

	// Apply project overrides
	if data, err := os.ReadFile(pm.projectPath); err == nil {
		var projectProfile UserProfile
		if json.Unmarshal(data, &projectProfile) == nil {
			pm.mergeProfiles(profile, &projectProfile)
		}
	}

	// Ensure conventions map is initialized
	if profile.Conventions == nil {
		profile.Conventions = make(map[string]string)
	}

	return profile, nil
}

// Save saves profile to either global or project path.
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

// RecordCorrection learns from a user correction.
// If the pattern was seen before, its count is incremented.
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

// SetPreference sets a convention preference.
func (profile *UserProfile) SetPreference(key, value string) {
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if profile.Conventions == nil {
		profile.Conventions = make(map[string]string)
	}
	profile.Conventions[key] = value
}

// GetPreference gets a convention preference.
func (profile *UserProfile) GetPreference(key string) string {
	profile.mu.RLock()
	defer profile.mu.RUnlock()
	if profile.Conventions == nil {
		return ""
	}
	return profile.Conventions[key]
}

// AddFramework adds a framework preference if not already present.
func (profile *UserProfile) AddFramework(framework string) {
	profile.mu.Lock()
	defer profile.mu.Unlock()

	for _, f := range profile.Frameworks {
		if f == framework {
			return
		}
	}
	profile.Frameworks = append(profile.Frameworks, framework)
}

// AddCodePattern adds a code pattern preference if not already present.
func (profile *UserProfile) AddCodePattern(pattern string) {
	profile.mu.Lock()
	defer profile.mu.Unlock()

	for _, p := range profile.CodePatterns {
		if p == pattern {
			return
		}
	}
	profile.CodePatterns = append(profile.CodePatterns, pattern)
}

// GetCorrections returns a copy of all corrections.
func (profile *UserProfile) GetCorrections() []Correction {
	profile.mu.RLock()
	defer profile.mu.RUnlock()

	result := make([]Correction, len(profile.Corrections))
	copy(result, profile.Corrections)
	return result
}

// mergeProfiles applies project overrides to base profile.
func (pm *ProfileManager) mergeProfiles(base, override *UserProfile) {
	if override.Verbosity != "" {
		base.Verbosity = override.Verbosity
	}

	// Merge frameworks (avoid duplicates)
	for _, f := range override.Frameworks {
		found := false
		for _, bf := range base.Frameworks {
			if bf == f {
				found = true
				break
			}
		}
		if !found {
			base.Frameworks = append(base.Frameworks, f)
		}
	}

	// Merge conventions (override wins)
	if base.Conventions == nil {
		base.Conventions = make(map[string]string)
	}
	for k, v := range override.Conventions {
		base.Conventions[k] = v
	}

	// Merge code patterns (avoid duplicates)
	for _, p := range override.CodePatterns {
		found := false
		for _, bp := range base.CodePatterns {
			if bp == p {
				found = true
				break
			}
		}
		if !found {
			base.CodePatterns = append(base.CodePatterns, p)
		}
	}

	// Merge corrections (combine counts for same patterns)
	for _, oc := range override.Corrections {
		found := false
		for i := range base.Corrections {
			if base.Corrections[i].Pattern == oc.Pattern {
				base.Corrections[i].Count += oc.Count
				base.Corrections[i].Correction = oc.Correction
				found = true
				break
			}
		}
		if !found {
			base.Corrections = append(base.Corrections, oc)
		}
	}
}

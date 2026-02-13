package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserProfile_RecordCorrection(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// First correction
	profile.RecordCorrection("use tabs", "prefer spaces")
	if len(profile.Corrections) != 1 {
		t.Errorf("expected 1 correction, got %d", len(profile.Corrections))
	}
	if profile.Corrections[0].Count != 1 {
		t.Errorf("expected count 1, got %d", profile.Corrections[0].Count)
	}

	// Same pattern again
	profile.RecordCorrection("use tabs", "always use spaces")
	if len(profile.Corrections) != 1 {
		t.Errorf("expected 1 correction after duplicate, got %d", len(profile.Corrections))
	}
	if profile.Corrections[0].Count != 2 {
		t.Errorf("expected count 2 after duplicate, got %d", profile.Corrections[0].Count)
	}
	if profile.Corrections[0].Correction != "always use spaces" {
		t.Errorf("expected updated correction text")
	}

	// Different pattern
	profile.RecordCorrection("long names", "use short names")
	if len(profile.Corrections) != 2 {
		t.Errorf("expected 2 corrections, got %d", len(profile.Corrections))
	}
}

func TestUserProfile_Preferences(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// Set and get preference
	profile.SetPreference("indent", "tabs")
	if got := profile.GetPreference("indent"); got != "tabs" {
		t.Errorf("expected 'tabs', got '%s'", got)
	}

	// Get non-existent preference
	if got := profile.GetPreference("nonexistent"); got != "" {
		t.Errorf("expected empty string for non-existent key, got '%s'", got)
	}
}

func TestUserProfile_AddFramework(t *testing.T) {
	profile := &UserProfile{}

	profile.AddFramework("gin")
	profile.AddFramework("gorm")
	profile.AddFramework("gin") // duplicate

	if len(profile.Frameworks) != 2 {
		t.Errorf("expected 2 frameworks, got %d", len(profile.Frameworks))
	}
}

func TestUserProfile_AddCodePattern(t *testing.T) {
	profile := &UserProfile{}

	profile.AddCodePattern("early_returns")
	profile.AddCodePattern("guard_clauses")
	profile.AddCodePattern("early_returns") // duplicate

	if len(profile.CodePatterns) != 2 {
		t.Errorf("expected 2 code patterns, got %d", len(profile.CodePatterns))
	}
}

func TestProfileManager_LoadSave(t *testing.T) {
	tmpDir := t.TempDir()

	globalPath := filepath.Join(tmpDir, "global", "profile.json")
	projectPath := filepath.Join(tmpDir, "project", ".user-profile.json")

	pm := NewProfileManager(globalPath, projectPath)

	// Save global profile
	globalProfile := &UserProfile{
		Verbosity:   "concise",
		Frameworks:  []string{"gin"},
		Conventions: map[string]string{"indent": "tabs"},
	}
	if err := pm.Save(globalProfile, true); err != nil {
		t.Fatalf("failed to save global profile: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		t.Error("global profile file was not created")
	}

	// Save project profile
	projectProfile := &UserProfile{
		Verbosity:   "detailed",
		Frameworks:  []string{"echo"},
		Conventions: map[string]string{"line_length": "100"},
	}
	if err := pm.Save(projectProfile, false); err != nil {
		t.Fatalf("failed to save project profile: %v", err)
	}

	// Load and verify merge
	loaded, err := pm.Load()
	if err != nil {
		t.Fatalf("failed to load profile: %v", err)
	}

	// Project verbosity should override global
	if loaded.Verbosity != "detailed" {
		t.Errorf("expected verbosity 'detailed', got '%s'", loaded.Verbosity)
	}

	// Frameworks should be merged
	if len(loaded.Frameworks) != 2 {
		t.Errorf("expected 2 merged frameworks, got %d", len(loaded.Frameworks))
	}

	// Conventions should be merged
	if loaded.Conventions["indent"] != "tabs" {
		t.Errorf("expected indent 'tabs' from global, got '%s'", loaded.Conventions["indent"])
	}
	if loaded.Conventions["line_length"] != "100" {
		t.Errorf("expected line_length '100' from project, got '%s'", loaded.Conventions["line_length"])
	}
}

func TestProfileManager_LoadEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	// Non-existent paths
	pm := NewProfileManager(
		filepath.Join(tmpDir, "nonexistent", "global.json"),
		filepath.Join(tmpDir, "nonexistent", "project.json"),
	)

	profile, err := pm.Load()
	if err != nil {
		t.Fatalf("Load should not error for missing files: %v", err)
	}

	// Should return empty but initialized profile
	if profile.Conventions == nil {
		t.Error("Conventions map should be initialized")
	}
}

func TestProfileManager_MergeCorrections(t *testing.T) {
	tmpDir := t.TempDir()

	globalPath := filepath.Join(tmpDir, "global", "profile.json")
	projectPath := filepath.Join(tmpDir, "project", "profile.json")

	pm := NewProfileManager(globalPath, projectPath)

	// Save global with correction
	globalProfile := &UserProfile{
		Conventions: make(map[string]string),
		Corrections: []Correction{
			{Pattern: "pattern1", Correction: "fix1", Count: 2},
		},
	}
	if err := pm.Save(globalProfile, true); err != nil {
		t.Fatalf("failed to save global: %v", err)
	}

	// Save project with same and different corrections
	projectProfile := &UserProfile{
		Conventions: make(map[string]string),
		Corrections: []Correction{
			{Pattern: "pattern1", Correction: "fix1-updated", Count: 3},
			{Pattern: "pattern2", Correction: "fix2", Count: 1},
		},
	}
	if err := pm.Save(projectProfile, false); err != nil {
		t.Fatalf("failed to save project: %v", err)
	}

	// Load and verify
	loaded, err := pm.Load()
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(loaded.Corrections) != 2 {
		t.Errorf("expected 2 merged corrections, got %d", len(loaded.Corrections))
	}

	// Find pattern1 and verify count is combined
	for _, c := range loaded.Corrections {
		if c.Pattern == "pattern1" {
			if c.Count != 5 { // 2 + 3
				t.Errorf("expected combined count 5 for pattern1, got %d", c.Count)
			}
			if c.Correction != "fix1-updated" {
				t.Errorf("expected project correction to override, got '%s'", c.Correction)
			}
		}
	}
}

func TestUserProfile_GetCorrections(t *testing.T) {
	profile := &UserProfile{
		Corrections: []Correction{
			{Pattern: "p1", Correction: "c1", Count: 1},
			{Pattern: "p2", Correction: "c2", Count: 2},
		},
	}

	corrections := profile.GetCorrections()

	if len(corrections) != 2 {
		t.Errorf("expected 2 corrections, got %d", len(corrections))
	}

	// Verify it's a copy
	corrections[0].Count = 999
	if profile.Corrections[0].Count == 999 {
		t.Error("GetCorrections should return a copy")
	}
}

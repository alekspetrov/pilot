package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewProfileManager(t *testing.T) {
	pm := NewProfileManager("/global/path", "/project/path")

	if pm.globalPath != "/global/path" {
		t.Errorf("expected globalPath /global/path, got %s", pm.globalPath)
	}
	if pm.projectPath != "/project/path" {
		t.Errorf("expected projectPath /project/path, got %s", pm.projectPath)
	}
}

func TestProfileManager_Load_NoFiles(t *testing.T) {
	pm := NewProfileManager("/nonexistent/global.json", "/nonexistent/project.json")

	profile, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if profile == nil {
		t.Fatal("expected non-nil profile")
	}
	if profile.Conventions == nil {
		t.Error("expected Conventions to be initialized")
	}
}

func TestProfileManager_Load_WithGlobalProfile(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.json")
	projectPath := filepath.Join(tmpDir, "project.json")

	// Create global profile
	globalData := `{"verbosity":"detailed","frameworks":["gin"],"conventions":{"indent":"tabs"}}`
	if err := os.WriteFile(globalPath, []byte(globalData), 0644); err != nil {
		t.Fatal(err)
	}

	pm := NewProfileManager(globalPath, projectPath)
	profile, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if profile.Verbosity != "detailed" {
		t.Errorf("expected verbosity 'detailed', got %s", profile.Verbosity)
	}
	if len(profile.Frameworks) != 1 || profile.Frameworks[0] != "gin" {
		t.Errorf("expected frameworks [gin], got %v", profile.Frameworks)
	}
	if profile.Conventions["indent"] != "tabs" {
		t.Errorf("expected indent=tabs, got %s", profile.Conventions["indent"])
	}
}

func TestProfileManager_Load_WithProjectOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.json")
	projectPath := filepath.Join(tmpDir, "project.json")

	// Create global profile
	globalData := `{"verbosity":"detailed","frameworks":["gin"],"conventions":{"indent":"tabs"}}`
	if err := os.WriteFile(globalPath, []byte(globalData), 0644); err != nil {
		t.Fatal(err)
	}

	// Create project profile (overrides)
	projectData := `{"verbosity":"concise","frameworks":["gorm"],"conventions":{"indent":"spaces"}}`
	if err := os.WriteFile(projectPath, []byte(projectData), 0644); err != nil {
		t.Fatal(err)
	}

	pm := NewProfileManager(globalPath, projectPath)
	profile, err := pm.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verbosity should be overridden
	if profile.Verbosity != "concise" {
		t.Errorf("expected verbosity 'concise', got %s", profile.Verbosity)
	}
	// Frameworks should be merged
	if len(profile.Frameworks) != 2 {
		t.Errorf("expected 2 frameworks, got %d", len(profile.Frameworks))
	}
	// Conventions should be overridden
	if profile.Conventions["indent"] != "spaces" {
		t.Errorf("expected indent=spaces, got %s", profile.Conventions["indent"])
	}
}

func TestProfileManager_Save(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global", "profile.json")
	projectPath := filepath.Join(tmpDir, "project", "profile.json")

	pm := NewProfileManager(globalPath, projectPath)

	profile := &UserProfile{
		Verbosity:   "detailed",
		Frameworks:  []string{"gin"},
		Conventions: map[string]string{"indent": "tabs"},
	}

	// Save to project path
	if err := pm.Save(profile, false); err != nil {
		t.Fatalf("Save(global=false) error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Error("expected project profile file to exist")
	}

	// Save to global path
	if err := pm.Save(profile, true); err != nil {
		t.Fatalf("Save(global=true) error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(globalPath); os.IsNotExist(err) {
		t.Error("expected global profile file to exist")
	}
}

func TestUserProfile_RecordCorrection(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	// First correction
	profile.RecordCorrection("tabs", "use spaces")
	if len(profile.Corrections) != 1 {
		t.Fatalf("expected 1 correction, got %d", len(profile.Corrections))
	}
	if profile.Corrections[0].Count != 1 {
		t.Errorf("expected count 1, got %d", profile.Corrections[0].Count)
	}

	// Same pattern again - should increment count
	profile.RecordCorrection("tabs", "use spaces")
	if len(profile.Corrections) != 1 {
		t.Errorf("expected still 1 correction, got %d", len(profile.Corrections))
	}
	if profile.Corrections[0].Count != 2 {
		t.Errorf("expected count 2, got %d", profile.Corrections[0].Count)
	}

	// Different pattern
	profile.RecordCorrection("semicolons", "no semicolons needed")
	if len(profile.Corrections) != 2 {
		t.Errorf("expected 2 corrections, got %d", len(profile.Corrections))
	}
}

func TestUserProfile_SetGetPreference(t *testing.T) {
	profile := &UserProfile{
		Conventions: make(map[string]string),
	}

	profile.SetPreference("indent", "tabs")
	got := profile.GetPreference("indent")
	if got != "tabs" {
		t.Errorf("expected 'tabs', got %s", got)
	}

	// Non-existent key
	got = profile.GetPreference("nonexistent")
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

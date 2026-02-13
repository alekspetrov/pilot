package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCreateMarker(t *testing.T) {
	tmpDir := t.TempDir()

	marker := &ContextMarker{
		Description:   "Test checkpoint",
		TaskID:        "TASK-01",
		FilesModified: []string{"main.go", "runner.go"},
		Commits:       []string{"abc123"},
		CurrentFocus:  "Implementing feature X",
	}

	err := CreateMarker(tmpDir, marker)
	if err != nil {
		t.Fatalf("CreateMarker() error = %v", err)
	}

	// Verify file was created
	if marker.FilePath == "" {
		t.Fatal("marker.FilePath is empty")
	}

	if _, err := os.Stat(marker.FilePath); os.IsNotExist(err) {
		t.Fatalf("marker file not created at %s", marker.FilePath)
	}

	// Verify .active file was updated
	active, err := GetActiveMarker(tmpDir)
	if err != nil {
		t.Fatalf("GetActiveMarker() error = %v", err)
	}
	if active != marker.FilePath {
		t.Errorf("active marker = %q, want %q", active, marker.FilePath)
	}
}

func TestSetAndGetActiveMarker(t *testing.T) {
	tmpDir := t.TempDir()
	markersDir := filepath.Join(tmpDir, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatal(err)
	}

	testPath := "/path/to/marker.md"
	err := SetActiveMarker(tmpDir, testPath)
	if err != nil {
		t.Fatalf("SetActiveMarker() error = %v", err)
	}

	got, err := GetActiveMarker(tmpDir)
	if err != nil {
		t.Fatalf("GetActiveMarker() error = %v", err)
	}
	if got != testPath {
		t.Errorf("GetActiveMarker() = %q, want %q", got, testPath)
	}
}

func TestLoadMarker(t *testing.T) {
	tmpDir := t.TempDir()
	markerPath := filepath.Join(tmpDir, "test-marker.md")

	content := `# Context Marker: Test description

**Created:** 2026-02-13 10:30

**Task:** TASK-42

## Current Focus

Working on feature Y

## Files Modified

- file1.go
- file2.go

## Commits

- commit1
- commit2
`
	if err := os.WriteFile(markerPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	marker, err := LoadMarker(markerPath)
	if err != nil {
		t.Fatalf("LoadMarker() error = %v", err)
	}

	if marker.Description != "Test description" {
		t.Errorf("Description = %q, want %q", marker.Description, "Test description")
	}
	if marker.TaskID != "TASK-42" {
		t.Errorf("TaskID = %q, want %q", marker.TaskID, "TASK-42")
	}
	if marker.CurrentFocus != "Working on feature Y" {
		t.Errorf("CurrentFocus = %q, want %q", marker.CurrentFocus, "Working on feature Y")
	}
	if len(marker.FilesModified) != 2 {
		t.Errorf("len(FilesModified) = %d, want 2", len(marker.FilesModified))
	}
	if len(marker.Commits) != 2 {
		t.Errorf("len(Commits) = %d, want 2", len(marker.Commits))
	}
}

func TestListMarkers(t *testing.T) {
	tmpDir := t.TempDir()
	markersDir := filepath.Join(tmpDir, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test marker files with different timestamps
	markers := []struct {
		filename string
		created  string
	}{
		{"2026-02-10-0900_older.md", "2026-02-10 09:00"},
		{"2026-02-13-1500_newer.md", "2026-02-13 15:00"},
		{"2026-02-12-1200_middle.md", "2026-02-12 12:00"},
	}

	for _, m := range markers {
		content := "# Context Marker: " + m.filename + "\n\n**Created:** " + m.created + "\n\n## Current Focus\n\ntest\n"
		if err := os.WriteFile(filepath.Join(markersDir, m.filename), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	result, err := ListMarkers(tmpDir)
	if err != nil {
		t.Fatalf("ListMarkers() error = %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3", len(result))
	}

	// Should be sorted newest first
	if result[0].Name != "2026-02-13-1500_newer.md" {
		t.Errorf("result[0].Name = %q, want newest marker", result[0].Name)
	}
	if result[2].Name != "2026-02-10-0900_older.md" {
		t.Errorf("result[2].Name = %q, want oldest marker", result[2].Name)
	}
}

func TestCleanupOldMarkers(t *testing.T) {
	tmpDir := t.TempDir()
	markersDir := filepath.Join(tmpDir, ".context-markers")
	if err := os.MkdirAll(markersDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create an old marker (30 days ago)
	oldTime := time.Now().AddDate(0, 0, -30)
	oldContent := "# Context Marker: old\n\n**Created:** " + oldTime.Format("2006-01-02 15:04") + "\n\n## Current Focus\n\nold\n"
	oldPath := filepath.Join(markersDir, "old_marker.md")
	if err := os.WriteFile(oldPath, []byte(oldContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a recent marker
	newTime := time.Now()
	newContent := "# Context Marker: new\n\n**Created:** " + newTime.Format("2006-01-02 15:04") + "\n\n## Current Focus\n\nnew\n"
	newPath := filepath.Join(markersDir, "new_marker.md")
	if err := os.WriteFile(newPath, []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Cleanup markers older than 7 days
	err := CleanupOldMarkers(tmpDir, 7)
	if err != nil {
		t.Fatalf("CleanupOldMarkers() error = %v", err)
	}

	// Old marker should be deleted
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("old marker should have been deleted")
	}

	// New marker should still exist
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("new marker should still exist")
	}
}

func TestSanitizeFilename(t *testing.T) {
	longInput := ""
	for i := 0; i < 100; i++ {
		longInput += "a"
	}
	longExpected := ""
	for i := 0; i < 50; i++ {
		longExpected += "a"
	}

	tests := []struct {
		input string
		want  string
	}{
		{"Simple Test", "simple-test"},
		{"Test/With/Slashes", "testwithslashes"},
		{"Special!@#$%^&*()Characters", "specialcharacters"},
		{"normal-name", "normal-name"},
		{"UPPERCASE", "uppercase"},
		{longInput, longExpected}, // truncation test
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

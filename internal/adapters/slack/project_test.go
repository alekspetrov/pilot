package slack

import (
	"testing"
)

func TestDefaultProjectInfo(t *testing.T) {
	p := DefaultProjectInfo("my-project")

	if p.Name != "my-project" {
		t.Errorf("Expected Name='my-project', got %q", p.Name)
	}
	if p.Branch != "main" {
		t.Errorf("Expected Branch='main', got %q", p.Branch)
	}
	if !p.Enabled {
		t.Error("Expected Enabled=true")
	}
	if len(p.Labels) != 1 || p.Labels[0] != "pilot" {
		t.Errorf("Expected Labels=['pilot'], got %v", p.Labels)
	}
}

func TestStaticProjectSource_GetProject(t *testing.T) {
	projects := []*ProjectInfo{
		{
			Name:       "pilot",
			ChannelID:  "C123456789",
			Repository: "github.com/example/pilot",
			WorkDir:    "/home/user/pilot",
			Branch:     "main",
			Enabled:    true,
		},
		{
			Name:       "backend",
			ChannelID:  "C987654321",
			Repository: "github.com/example/backend",
			WorkDir:    "/home/user/backend",
			Branch:     "develop",
			Enabled:    true,
		},
	}

	source := NewStaticProjectSource(projects)

	// Test finding existing project
	p, err := source.GetProject("C123456789")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("Expected project to be found")
	}
	if p.Name != "pilot" {
		t.Errorf("Expected project name 'pilot', got %q", p.Name)
	}

	// Test finding second project
	p2, err := source.GetProject("C987654321")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p2 == nil {
		t.Fatal("Expected project to be found")
	}
	if p2.Name != "backend" {
		t.Errorf("Expected project name 'backend', got %q", p2.Name)
	}

	// Test non-existent channel
	p3, err := source.GetProject("C000000000")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p3 != nil {
		t.Error("Expected nil for non-existent channel")
	}
}

func TestStaticProjectSource_ListProjects(t *testing.T) {
	projects := []*ProjectInfo{
		{Name: "project1", ChannelID: "C111"},
		{Name: "project2", ChannelID: "C222"},
		{Name: "project3", ChannelID: "C333"},
	}

	source := NewStaticProjectSource(projects)
	listed := source.ListProjects()

	if len(listed) != 3 {
		t.Errorf("Expected 3 projects, got %d", len(listed))
	}

	// Verify same projects returned
	for i, p := range listed {
		if p != projects[i] {
			t.Errorf("Project %d mismatch", i)
		}
	}
}

func TestStaticProjectSource_EmptyChannelID(t *testing.T) {
	// Projects without channel ID should still be in ListProjects
	// but not findable via GetProject
	projects := []*ProjectInfo{
		{Name: "with-channel", ChannelID: "C123"},
		{Name: "no-channel", ChannelID: ""},
	}

	source := NewStaticProjectSource(projects)

	// ListProjects returns all
	if len(source.ListProjects()) != 2 {
		t.Errorf("Expected 2 projects in list")
	}

	// GetProject with empty string returns nil
	p, _ := source.GetProject("")
	if p != nil {
		t.Error("Expected nil for empty channel ID lookup")
	}
}

func TestStaticProjectSource_EmptyProjects(t *testing.T) {
	source := NewStaticProjectSource(nil)

	listed := source.ListProjects()
	if len(listed) != 0 {
		t.Errorf("Expected empty list, got %v", listed)
	}

	p, err := source.GetProject("C123")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p != nil {
		t.Error("Expected nil for any channel lookup on empty source")
	}
}

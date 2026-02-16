package research

import (
	"testing"
)

func TestNoteCategory(t *testing.T) {
	tests := []struct {
		name     string
		category NoteCategory
		want     string
	}{
		{
			name:     "marketing category",
			category: NoteCategoryMarketing,
			want:     "marketing",
		},
		{
			name:     "design category",
			category: NoteCategoryDesign,
			want:     "design",
		},
		{
			name:     "keep category",
			category: NoteCategoryKeep,
			want:     "keep",
		},
		{
			name:     "improve category",
			category: NoteCategoryImprove,
			want:     "improve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.category); got != tt.want {
				t.Errorf("NoteCategory = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateResearchInput(t *testing.T) {
	input := CreateResearchInput{
		Name:              "Test Research",
		Description:       "A test research description",
		OwnAppID:          "com.example.app",
		OwnAppName:        "Example App",
		OwnAppIconURL:     "https://example.com/icon.png",
		OwnAppScreenshots: []string{"https://example.com/s1.png", "https://example.com/s2.png"},
	}

	if input.Name == "" {
		t.Error("Name should not be empty")
	}
	if input.OwnAppID != "com.example.app" {
		t.Errorf("OwnAppID = %v, want %v", input.OwnAppID, "com.example.app")
	}
	if len(input.OwnAppScreenshots) != 2 {
		t.Errorf("OwnAppScreenshots length = %v, want %v", len(input.OwnAppScreenshots), 2)
	}
}

func TestCreateNoteInput(t *testing.T) {
	input := CreateNoteInput{
		Category:  NoteCategoryKeep,
		Content:   "Keep this feature",
		Color:     "green",
		PositionX: 100,
		PositionY: 200,
	}

	if input.Category != NoteCategoryKeep {
		t.Errorf("Category = %v, want %v", input.Category, NoteCategoryKeep)
	}
	if input.Content != "Keep this feature" {
		t.Errorf("Content = %v, want %v", input.Content, "Keep this feature")
	}
}

func TestAddCompetitorInput(t *testing.T) {
	input := AddCompetitorInput{
		AppID:       "com.competitor.app",
		Name:        "Competitor App",
		IconURL:     "https://competitor.com/icon.png",
		Screenshots: []string{"https://competitor.com/s1.png"},
	}

	if input.AppID == "" {
		t.Error("AppID should not be empty")
	}
	if len(input.Screenshots) != 1 {
		t.Errorf("Screenshots length = %v, want %v", len(input.Screenshots), 1)
	}
}

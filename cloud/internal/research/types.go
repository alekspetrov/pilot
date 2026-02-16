package research

import (
	"time"

	"github.com/google/uuid"
)

// Research represents a competitor analysis research session
type Research struct {
	ID                uuid.UUID        `json:"id"`
	OrgID             uuid.UUID        `json:"org_id"`
	Name              string           `json:"name"`
	Description       string           `json:"description,omitempty"`
	OwnAppID          string           `json:"own_app_id,omitempty"`
	OwnAppName        string           `json:"own_app_name,omitempty"`
	OwnAppIconURL     string           `json:"own_app_icon_url,omitempty"`
	OwnAppScreenshots []string         `json:"own_app_screenshots,omitempty"`
	Settings          ResearchSettings `json:"settings"`
	CreatedBy         uuid.UUID        `json:"created_by"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// ResearchSettings holds research-specific configuration
type ResearchSettings struct {
	AutoFetchMetadata bool `json:"auto_fetch_metadata"`
}

// NoteCategory defines the type of note
type NoteCategory string

const (
	NoteCategoryMarketing NoteCategory = "marketing"
	NoteCategoryDesign    NoteCategory = "design"
	NoteCategoryKeep      NoteCategory = "keep"
	NoteCategoryImprove   NoteCategory = "improve"
)

// OwnAppNote represents a sticky note for own app analysis
type OwnAppNote struct {
	ID         uuid.UUID    `json:"id"`
	ResearchID uuid.UUID    `json:"research_id"`
	Category   NoteCategory `json:"category"`
	Content    string       `json:"content"`
	Color      string       `json:"color"`
	PositionX  int          `json:"position_x"`
	PositionY  int          `json:"position_y"`
	CreatedBy  uuid.UUID    `json:"created_by"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}

// CompetitorApp represents a competitor app being analyzed
type CompetitorApp struct {
	ID          uuid.UUID `json:"id"`
	ResearchID  uuid.UUID `json:"research_id"`
	AppID       string    `json:"app_id"`
	Name        string    `json:"name,omitempty"`
	IconURL     string    `json:"icon_url,omitempty"`
	Screenshots []string  `json:"screenshots,omitempty"`
	Notes       []string  `json:"notes,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateResearchInput holds data for creating a research
type CreateResearchInput struct {
	Name              string   `json:"name"`
	Description       string   `json:"description,omitempty"`
	OwnAppID          string   `json:"own_app_id,omitempty"`
	OwnAppName        string   `json:"own_app_name,omitempty"`
	OwnAppIconURL     string   `json:"own_app_icon_url,omitempty"`
	OwnAppScreenshots []string `json:"own_app_screenshots,omitempty"`
}

// UpdateResearchInput holds data for updating a research
type UpdateResearchInput struct {
	Name              *string  `json:"name,omitempty"`
	Description       *string  `json:"description,omitempty"`
	OwnAppID          *string  `json:"own_app_id,omitempty"`
	OwnAppName        *string  `json:"own_app_name,omitempty"`
	OwnAppIconURL     *string  `json:"own_app_icon_url,omitempty"`
	OwnAppScreenshots []string `json:"own_app_screenshots,omitempty"`
}

// CreateNoteInput holds data for creating an own app note
type CreateNoteInput struct {
	Category  NoteCategory `json:"category"`
	Content   string       `json:"content"`
	Color     string       `json:"color,omitempty"`
	PositionX int          `json:"position_x,omitempty"`
	PositionY int          `json:"position_y,omitempty"`
}

// UpdateNoteInput holds data for updating an own app note
type UpdateNoteInput struct {
	Content   *string `json:"content,omitempty"`
	Color     *string `json:"color,omitempty"`
	PositionX *int    `json:"position_x,omitempty"`
	PositionY *int    `json:"position_y,omitempty"`
}

// AddCompetitorInput holds data for adding a competitor app
type AddCompetitorInput struct {
	AppID       string   `json:"app_id"`
	Name        string   `json:"name,omitempty"`
	IconURL     string   `json:"icon_url,omitempty"`
	Screenshots []string `json:"screenshots,omitempty"`
}

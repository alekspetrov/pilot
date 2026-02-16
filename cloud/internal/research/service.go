package research

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Service provides research business logic
type Service struct {
	store *Store
}

// NewService creates a new research service
func NewService(store *Store) *Service {
	return &Service{store: store}
}

// CreateResearch creates a new research
func (s *Service) CreateResearch(ctx context.Context, orgID, userID uuid.UUID, input CreateResearchInput) (*Research, error) {
	if input.Name == "" {
		return nil, fmt.Errorf("research name is required")
	}

	now := time.Now()
	research := &Research{
		ID:                uuid.New(),
		OrgID:             orgID,
		Name:              input.Name,
		Description:       input.Description,
		OwnAppID:          input.OwnAppID,
		OwnAppName:        input.OwnAppName,
		OwnAppIconURL:     input.OwnAppIconURL,
		OwnAppScreenshots: input.OwnAppScreenshots,
		Settings:          ResearchSettings{AutoFetchMetadata: true},
		CreatedBy:         userID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	if err := s.store.CreateResearch(ctx, research); err != nil {
		return nil, err
	}

	return research, nil
}

// GetResearch retrieves a research by ID
func (s *Service) GetResearch(ctx context.Context, id uuid.UUID) (*Research, error) {
	return s.store.GetResearch(ctx, id)
}

// ListResearches returns all researches for an organization
func (s *Service) ListResearches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*Research, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return s.store.ListResearches(ctx, orgID, limit, offset)
}

// UpdateResearch updates a research
func (s *Service) UpdateResearch(ctx context.Context, id uuid.UUID, input UpdateResearchInput) (*Research, error) {
	research, err := s.store.GetResearch(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		research.Name = *input.Name
	}
	if input.Description != nil {
		research.Description = *input.Description
	}
	if input.OwnAppID != nil {
		research.OwnAppID = *input.OwnAppID
	}
	if input.OwnAppName != nil {
		research.OwnAppName = *input.OwnAppName
	}
	if input.OwnAppIconURL != nil {
		research.OwnAppIconURL = *input.OwnAppIconURL
	}
	if input.OwnAppScreenshots != nil {
		research.OwnAppScreenshots = input.OwnAppScreenshots
	}

	if err := s.store.UpdateResearch(ctx, research); err != nil {
		return nil, err
	}

	return research, nil
}

// DeleteResearch deletes a research
func (s *Service) DeleteResearch(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteResearch(ctx, id)
}

// SetOwnApp sets or updates the own app details for a research
func (s *Service) SetOwnApp(ctx context.Context, researchID uuid.UUID, appID, appName, iconURL string, screenshots []string) (*Research, error) {
	research, err := s.store.GetResearch(ctx, researchID)
	if err != nil {
		return nil, err
	}

	research.OwnAppID = appID
	research.OwnAppName = appName
	research.OwnAppIconURL = iconURL
	research.OwnAppScreenshots = screenshots

	if err := s.store.UpdateResearch(ctx, research); err != nil {
		return nil, err
	}

	return research, nil
}

// --- Own App Notes ---

// CreateNote creates a new sticky note
func (s *Service) CreateNote(ctx context.Context, researchID, userID uuid.UUID, input CreateNoteInput) (*OwnAppNote, error) {
	if input.Content == "" {
		return nil, fmt.Errorf("note content is required")
	}
	if input.Category == "" {
		return nil, fmt.Errorf("note category is required")
	}

	color := input.Color
	if color == "" {
		color = "yellow"
	}

	now := time.Now()
	note := &OwnAppNote{
		ID:         uuid.New(),
		ResearchID: researchID,
		Category:   input.Category,
		Content:    input.Content,
		Color:      color,
		PositionX:  input.PositionX,
		PositionY:  input.PositionY,
		CreatedBy:  userID,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if err := s.store.CreateNote(ctx, note); err != nil {
		return nil, err
	}

	return note, nil
}

// GetNote retrieves a note by ID
func (s *Service) GetNote(ctx context.Context, id uuid.UUID) (*OwnAppNote, error) {
	return s.store.GetNote(ctx, id)
}

// ListNotes returns all notes for a research
func (s *Service) ListNotes(ctx context.Context, researchID uuid.UUID) ([]*OwnAppNote, error) {
	return s.store.ListNotes(ctx, researchID)
}

// ListNotesByCategory returns notes filtered by category
func (s *Service) ListNotesByCategory(ctx context.Context, researchID uuid.UUID, category NoteCategory) ([]*OwnAppNote, error) {
	return s.store.ListNotesByCategory(ctx, researchID, category)
}

// UpdateNote updates a note
func (s *Service) UpdateNote(ctx context.Context, id uuid.UUID, input UpdateNoteInput) (*OwnAppNote, error) {
	note, err := s.store.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}

	if input.Content != nil {
		note.Content = *input.Content
	}
	if input.Color != nil {
		note.Color = *input.Color
	}
	if input.PositionX != nil {
		note.PositionX = *input.PositionX
	}
	if input.PositionY != nil {
		note.PositionY = *input.PositionY
	}

	if err := s.store.UpdateNote(ctx, note); err != nil {
		return nil, err
	}

	return note, nil
}

// DeleteNote deletes a note
func (s *Service) DeleteNote(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteNote(ctx, id)
}

// --- Competitor Apps ---

// AddCompetitor adds a competitor app to a research
func (s *Service) AddCompetitor(ctx context.Context, researchID uuid.UUID, input AddCompetitorInput) (*CompetitorApp, error) {
	if input.AppID == "" {
		return nil, fmt.Errorf("app ID is required")
	}

	now := time.Now()
	competitor := &CompetitorApp{
		ID:          uuid.New(),
		ResearchID:  researchID,
		AppID:       input.AppID,
		Name:        input.Name,
		IconURL:     input.IconURL,
		Screenshots: input.Screenshots,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.store.CreateCompetitor(ctx, competitor); err != nil {
		return nil, err
	}

	return competitor, nil
}

// GetCompetitor retrieves a competitor by ID
func (s *Service) GetCompetitor(ctx context.Context, id uuid.UUID) (*CompetitorApp, error) {
	return s.store.GetCompetitor(ctx, id)
}

// ListCompetitors returns all competitor apps for a research
func (s *Service) ListCompetitors(ctx context.Context, researchID uuid.UUID) ([]*CompetitorApp, error) {
	return s.store.ListCompetitors(ctx, researchID)
}

// UpdateCompetitor updates a competitor app
func (s *Service) UpdateCompetitor(ctx context.Context, competitor *CompetitorApp) error {
	return s.store.UpdateCompetitor(ctx, competitor)
}

// DeleteCompetitor deletes a competitor app
func (s *Service) DeleteCompetitor(ctx context.Context, id uuid.UUID) error {
	return s.store.DeleteCompetitor(ctx, id)
}

// GetResearchWithDetails returns a research with all its notes and competitors
func (s *Service) GetResearchWithDetails(ctx context.Context, id uuid.UUID) (*Research, []*OwnAppNote, []*CompetitorApp, error) {
	research, err := s.store.GetResearch(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}

	notes, err := s.store.ListNotes(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}

	competitors, err := s.store.ListCompetitors(ctx, id)
	if err != nil {
		return nil, nil, nil, err
	}

	return research, notes, competitors, nil
}

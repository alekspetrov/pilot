package research

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mockStore implements a minimal mock for testing service logic
type mockStore struct {
	researches   map[uuid.UUID]*Research
	notes        map[uuid.UUID]*OwnAppNote
	competitors  map[uuid.UUID]*CompetitorApp
	createErr    error
	getErr       error
	updateErr    error
	deleteErr    error
}

func newMockStore() *mockStore {
	return &mockStore{
		researches:  make(map[uuid.UUID]*Research),
		notes:       make(map[uuid.UUID]*OwnAppNote),
		competitors: make(map[uuid.UUID]*CompetitorApp),
	}
}

func (m *mockStore) CreateResearch(ctx context.Context, research *Research) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.researches[research.ID] = research
	return nil
}

func (m *mockStore) GetResearch(ctx context.Context, id uuid.UUID) (*Research, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	r, ok := m.researches[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (m *mockStore) ListResearches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*Research, error) {
	var result []*Research
	for _, r := range m.researches {
		if r.OrgID == orgID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateResearch(ctx context.Context, research *Research) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.researches[research.ID]; !ok {
		return ErrNotFound
	}
	m.researches[research.ID] = research
	return nil
}

func (m *mockStore) DeleteResearch(ctx context.Context, id uuid.UUID) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	if _, ok := m.researches[id]; !ok {
		return ErrNotFound
	}
	delete(m.researches, id)
	return nil
}

func (m *mockStore) CreateNote(ctx context.Context, note *OwnAppNote) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.notes[note.ID] = note
	return nil
}

func (m *mockStore) GetNote(ctx context.Context, id uuid.UUID) (*OwnAppNote, error) {
	n, ok := m.notes[id]
	if !ok {
		return nil, ErrNotFound
	}
	return n, nil
}

func (m *mockStore) ListNotes(ctx context.Context, researchID uuid.UUID) ([]*OwnAppNote, error) {
	var result []*OwnAppNote
	for _, n := range m.notes {
		if n.ResearchID == researchID {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockStore) ListNotesByCategory(ctx context.Context, researchID uuid.UUID, category NoteCategory) ([]*OwnAppNote, error) {
	var result []*OwnAppNote
	for _, n := range m.notes {
		if n.ResearchID == researchID && n.Category == category {
			result = append(result, n)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateNote(ctx context.Context, note *OwnAppNote) error {
	if _, ok := m.notes[note.ID]; !ok {
		return ErrNotFound
	}
	m.notes[note.ID] = note
	return nil
}

func (m *mockStore) DeleteNote(ctx context.Context, id uuid.UUID) error {
	if _, ok := m.notes[id]; !ok {
		return ErrNotFound
	}
	delete(m.notes, id)
	return nil
}

func (m *mockStore) CreateCompetitor(ctx context.Context, competitor *CompetitorApp) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.competitors[competitor.ID] = competitor
	return nil
}

func (m *mockStore) GetCompetitor(ctx context.Context, id uuid.UUID) (*CompetitorApp, error) {
	c, ok := m.competitors[id]
	if !ok {
		return nil, ErrNotFound
	}
	return c, nil
}

func (m *mockStore) ListCompetitors(ctx context.Context, researchID uuid.UUID) ([]*CompetitorApp, error) {
	var result []*CompetitorApp
	for _, c := range m.competitors {
		if c.ResearchID == researchID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (m *mockStore) UpdateCompetitor(ctx context.Context, competitor *CompetitorApp) error {
	if _, ok := m.competitors[competitor.ID]; !ok {
		return ErrNotFound
	}
	m.competitors[competitor.ID] = competitor
	return nil
}

func (m *mockStore) DeleteCompetitor(ctx context.Context, id uuid.UUID) error {
	if _, ok := m.competitors[id]; !ok {
		return ErrNotFound
	}
	delete(m.competitors, id)
	return nil
}

// storeInterface defines the interface for the store
type storeInterface interface {
	CreateResearch(ctx context.Context, research *Research) error
	GetResearch(ctx context.Context, id uuid.UUID) (*Research, error)
	ListResearches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*Research, error)
	UpdateResearch(ctx context.Context, research *Research) error
	DeleteResearch(ctx context.Context, id uuid.UUID) error
	CreateNote(ctx context.Context, note *OwnAppNote) error
	GetNote(ctx context.Context, id uuid.UUID) (*OwnAppNote, error)
	ListNotes(ctx context.Context, researchID uuid.UUID) ([]*OwnAppNote, error)
	ListNotesByCategory(ctx context.Context, researchID uuid.UUID, category NoteCategory) ([]*OwnAppNote, error)
	UpdateNote(ctx context.Context, note *OwnAppNote) error
	DeleteNote(ctx context.Context, id uuid.UUID) error
	CreateCompetitor(ctx context.Context, competitor *CompetitorApp) error
	GetCompetitor(ctx context.Context, id uuid.UUID) (*CompetitorApp, error)
	ListCompetitors(ctx context.Context, researchID uuid.UUID) ([]*CompetitorApp, error)
	UpdateCompetitor(ctx context.Context, competitor *CompetitorApp) error
	DeleteCompetitor(ctx context.Context, id uuid.UUID) error
}

// testService wraps the service for testing with mock store
type testService struct {
	store storeInterface
}

func newTestService(store storeInterface) *testService {
	return &testService{store: store}
}

func (s *testService) CreateResearch(ctx context.Context, orgID, userID uuid.UUID, input CreateResearchInput) (*Research, error) {
	if input.Name == "" {
		return nil, ErrNotFound
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

func (s *testService) CreateNote(ctx context.Context, researchID, userID uuid.UUID, input CreateNoteInput) (*OwnAppNote, error) {
	if input.Content == "" {
		return nil, ErrNotFound
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

func (s *testService) AddCompetitor(ctx context.Context, researchID uuid.UUID, input AddCompetitorInput) (*CompetitorApp, error) {
	if input.AppID == "" {
		return nil, ErrNotFound
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

func TestServiceCreateResearch(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := newTestService(store)

	orgID := uuid.New()
	userID := uuid.New()

	tests := []struct {
		name    string
		input   CreateResearchInput
		wantErr bool
	}{
		{
			name: "valid research",
			input: CreateResearchInput{
				Name:        "Test Research",
				Description: "Test description",
				OwnAppID:    "com.example.app",
			},
			wantErr: false,
		},
		{
			name: "research with screenshots",
			input: CreateResearchInput{
				Name:              "Research with screenshots",
				OwnAppScreenshots: []string{"https://example.com/s1.png"},
			},
			wantErr: false,
		},
		{
			name:    "empty name",
			input:   CreateResearchInput{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			research, err := svc.CreateResearch(ctx, orgID, userID, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateResearch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if research == nil {
					t.Error("CreateResearch() returned nil research")
					return
				}
				if research.Name != tt.input.Name {
					t.Errorf("CreateResearch() name = %v, want %v", research.Name, tt.input.Name)
				}
				if research.OrgID != orgID {
					t.Errorf("CreateResearch() orgID = %v, want %v", research.OrgID, orgID)
				}
			}
		})
	}
}

func TestServiceCreateNote(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := newTestService(store)

	researchID := uuid.New()
	userID := uuid.New()

	tests := []struct {
		name    string
		input   CreateNoteInput
		wantErr bool
	}{
		{
			name: "valid keep note",
			input: CreateNoteInput{
				Category: NoteCategoryKeep,
				Content:  "Keep this feature",
				Color:    "green",
			},
			wantErr: false,
		},
		{
			name: "valid improve note",
			input: CreateNoteInput{
				Category:  NoteCategoryImprove,
				Content:   "Improve this aspect",
				PositionX: 100,
				PositionY: 200,
			},
			wantErr: false,
		},
		{
			name: "default color",
			input: CreateNoteInput{
				Category: NoteCategoryMarketing,
				Content:  "Marketing note",
			},
			wantErr: false,
		},
		{
			name: "empty content",
			input: CreateNoteInput{
				Category: NoteCategoryDesign,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			note, err := svc.CreateNote(ctx, researchID, userID, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateNote() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if note == nil {
					t.Error("CreateNote() returned nil note")
					return
				}
				if note.Content != tt.input.Content {
					t.Errorf("CreateNote() content = %v, want %v", note.Content, tt.input.Content)
				}
				if tt.input.Color == "" && note.Color != "yellow" {
					t.Errorf("CreateNote() default color = %v, want yellow", note.Color)
				}
			}
		})
	}
}

func TestServiceAddCompetitor(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()
	svc := newTestService(store)

	researchID := uuid.New()

	tests := []struct {
		name    string
		input   AddCompetitorInput
		wantErr bool
	}{
		{
			name: "valid competitor",
			input: AddCompetitorInput{
				AppID:   "com.competitor.app",
				Name:    "Competitor App",
				IconURL: "https://competitor.com/icon.png",
			},
			wantErr: false,
		},
		{
			name: "competitor with screenshots",
			input: AddCompetitorInput{
				AppID:       "com.another.app",
				Screenshots: []string{"https://example.com/s1.png", "https://example.com/s2.png"},
			},
			wantErr: false,
		},
		{
			name: "empty app ID",
			input: AddCompetitorInput{
				Name: "No App ID",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			competitor, err := svc.AddCompetitor(ctx, researchID, tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AddCompetitor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if competitor == nil {
					t.Error("AddCompetitor() returned nil competitor")
					return
				}
				if competitor.AppID != tt.input.AppID {
					t.Errorf("AddCompetitor() appID = %v, want %v", competitor.AppID, tt.input.AppID)
				}
			}
		})
	}
}

func TestMockStoreOperations(t *testing.T) {
	ctx := context.Background()
	store := newMockStore()

	orgID := uuid.New()
	userID := uuid.New()

	// Test research CRUD
	t.Run("research CRUD", func(t *testing.T) {
		research := &Research{
			ID:        uuid.New(),
			OrgID:     orgID,
			Name:      "Test Research",
			CreatedBy: userID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := store.CreateResearch(ctx, research); err != nil {
			t.Fatalf("CreateResearch() error = %v", err)
		}

		got, err := store.GetResearch(ctx, research.ID)
		if err != nil {
			t.Fatalf("GetResearch() error = %v", err)
		}
		if got.Name != research.Name {
			t.Errorf("GetResearch() name = %v, want %v", got.Name, research.Name)
		}

		list, err := store.ListResearches(ctx, orgID, 10, 0)
		if err != nil {
			t.Fatalf("ListResearches() error = %v", err)
		}
		if len(list) != 1 {
			t.Errorf("ListResearches() length = %v, want 1", len(list))
		}

		research.Name = "Updated Research"
		if err := store.UpdateResearch(ctx, research); err != nil {
			t.Fatalf("UpdateResearch() error = %v", err)
		}

		if err := store.DeleteResearch(ctx, research.ID); err != nil {
			t.Fatalf("DeleteResearch() error = %v", err)
		}

		_, err = store.GetResearch(ctx, research.ID)
		if err != ErrNotFound {
			t.Errorf("GetResearch() after delete error = %v, want ErrNotFound", err)
		}
	})

	// Test note CRUD
	t.Run("note CRUD", func(t *testing.T) {
		researchID := uuid.New()
		note := &OwnAppNote{
			ID:         uuid.New(),
			ResearchID: researchID,
			Category:   NoteCategoryKeep,
			Content:    "Test note",
			Color:      "yellow",
			CreatedBy:  userID,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := store.CreateNote(ctx, note); err != nil {
			t.Fatalf("CreateNote() error = %v", err)
		}

		got, err := store.GetNote(ctx, note.ID)
		if err != nil {
			t.Fatalf("GetNote() error = %v", err)
		}
		if got.Content != note.Content {
			t.Errorf("GetNote() content = %v, want %v", got.Content, note.Content)
		}

		list, err := store.ListNotes(ctx, researchID)
		if err != nil {
			t.Fatalf("ListNotes() error = %v", err)
		}
		if len(list) != 1 {
			t.Errorf("ListNotes() length = %v, want 1", len(list))
		}

		byCategory, err := store.ListNotesByCategory(ctx, researchID, NoteCategoryKeep)
		if err != nil {
			t.Fatalf("ListNotesByCategory() error = %v", err)
		}
		if len(byCategory) != 1 {
			t.Errorf("ListNotesByCategory() length = %v, want 1", len(byCategory))
		}

		if err := store.DeleteNote(ctx, note.ID); err != nil {
			t.Fatalf("DeleteNote() error = %v", err)
		}
	})

	// Test competitor CRUD
	t.Run("competitor CRUD", func(t *testing.T) {
		researchID := uuid.New()
		competitor := &CompetitorApp{
			ID:         uuid.New(),
			ResearchID: researchID,
			AppID:      "com.test.app",
			Name:       "Test App",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := store.CreateCompetitor(ctx, competitor); err != nil {
			t.Fatalf("CreateCompetitor() error = %v", err)
		}

		got, err := store.GetCompetitor(ctx, competitor.ID)
		if err != nil {
			t.Fatalf("GetCompetitor() error = %v", err)
		}
		if got.AppID != competitor.AppID {
			t.Errorf("GetCompetitor() appID = %v, want %v", got.AppID, competitor.AppID)
		}

		list, err := store.ListCompetitors(ctx, researchID)
		if err != nil {
			t.Fatalf("ListCompetitors() error = %v", err)
		}
		if len(list) != 1 {
			t.Errorf("ListCompetitors() length = %v, want 1", len(list))
		}

		if err := store.DeleteCompetitor(ctx, competitor.ID); err != nil {
			t.Fatalf("DeleteCompetitor() error = %v", err)
		}
	})
}

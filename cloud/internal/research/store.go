package research

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("not found")
)

// Store provides research data access
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a new research store
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// --- Research Operations ---

// CreateResearch creates a new research
func (s *Store) CreateResearch(ctx context.Context, research *Research) error {
	settings, _ := json.Marshal(research.Settings)
	screenshots, _ := json.Marshal(research.OwnAppScreenshots)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO researches (id, org_id, name, description, own_app_id, own_app_name, own_app_icon_url, own_app_screenshots, settings, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, research.ID, research.OrgID, research.Name, research.Description, research.OwnAppID,
		research.OwnAppName, research.OwnAppIconURL, screenshots, settings, research.CreatedBy, research.CreatedAt, research.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create research: %w", err)
	}
	return nil
}

// GetResearch retrieves a research by ID
func (s *Store) GetResearch(ctx context.Context, id uuid.UUID) (*Research, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, org_id, name, description, own_app_id, own_app_name, own_app_icon_url, own_app_screenshots, settings, created_by, created_at, updated_at
		FROM researches WHERE id = $1
	`, id)

	return s.scanResearch(row)
}

func (s *Store) scanResearch(row pgx.Row) (*Research, error) {
	var r Research
	var settingsJSON, screenshotsJSON []byte
	var description, ownAppID, ownAppName, ownAppIconURL *string

	err := row.Scan(&r.ID, &r.OrgID, &r.Name, &description, &ownAppID, &ownAppName, &ownAppIconURL,
		&screenshotsJSON, &settingsJSON, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if description != nil {
		r.Description = *description
	}
	if ownAppID != nil {
		r.OwnAppID = *ownAppID
	}
	if ownAppName != nil {
		r.OwnAppName = *ownAppName
	}
	if ownAppIconURL != nil {
		r.OwnAppIconURL = *ownAppIconURL
	}
	if settingsJSON != nil {
		_ = json.Unmarshal(settingsJSON, &r.Settings)
	}
	if screenshotsJSON != nil {
		_ = json.Unmarshal(screenshotsJSON, &r.OwnAppScreenshots)
	}

	return &r, nil
}

// ListResearches returns all researches for an organization
func (s *Store) ListResearches(ctx context.Context, orgID uuid.UUID, limit, offset int) ([]*Research, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, org_id, name, description, own_app_id, own_app_name, own_app_icon_url, own_app_screenshots, settings, created_by, created_at, updated_at
		FROM researches WHERE org_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var researches []*Research
	for rows.Next() {
		var r Research
		var settingsJSON, screenshotsJSON []byte
		var description, ownAppID, ownAppName, ownAppIconURL *string

		if err := rows.Scan(&r.ID, &r.OrgID, &r.Name, &description, &ownAppID, &ownAppName, &ownAppIconURL,
			&screenshotsJSON, &settingsJSON, &r.CreatedBy, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}

		if description != nil {
			r.Description = *description
		}
		if ownAppID != nil {
			r.OwnAppID = *ownAppID
		}
		if ownAppName != nil {
			r.OwnAppName = *ownAppName
		}
		if ownAppIconURL != nil {
			r.OwnAppIconURL = *ownAppIconURL
		}
		if settingsJSON != nil {
			_ = json.Unmarshal(settingsJSON, &r.Settings)
		}
		if screenshotsJSON != nil {
			_ = json.Unmarshal(screenshotsJSON, &r.OwnAppScreenshots)
		}

		researches = append(researches, &r)
	}

	return researches, nil
}

// UpdateResearch updates a research
func (s *Store) UpdateResearch(ctx context.Context, research *Research) error {
	settings, _ := json.Marshal(research.Settings)
	screenshots, _ := json.Marshal(research.OwnAppScreenshots)

	result, err := s.pool.Exec(ctx, `
		UPDATE researches
		SET name = $2, description = $3, own_app_id = $4, own_app_name = $5, own_app_icon_url = $6, own_app_screenshots = $7, settings = $8, updated_at = $9
		WHERE id = $1
	`, research.ID, research.Name, research.Description, research.OwnAppID, research.OwnAppName,
		research.OwnAppIconURL, screenshots, settings, time.Now())

	if err != nil {
		return fmt.Errorf("failed to update research: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteResearch deletes a research
func (s *Store) DeleteResearch(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM researches WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete research: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- OwnAppNote Operations ---

// CreateNote creates a new own app note
func (s *Store) CreateNote(ctx context.Context, note *OwnAppNote) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO own_app_notes (id, research_id, category, content, color, position_x, position_y, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, note.ID, note.ResearchID, note.Category, note.Content, note.Color, note.PositionX, note.PositionY, note.CreatedBy, note.CreatedAt, note.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create note: %w", err)
	}
	return nil
}

// GetNote retrieves a note by ID
func (s *Store) GetNote(ctx context.Context, id uuid.UUID) (*OwnAppNote, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, research_id, category, content, color, position_x, position_y, created_by, created_at, updated_at
		FROM own_app_notes WHERE id = $1
	`, id)

	var n OwnAppNote
	err := row.Scan(&n.ID, &n.ResearchID, &n.Category, &n.Content, &n.Color, &n.PositionX, &n.PositionY, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	return &n, nil
}

// ListNotes returns all notes for a research
func (s *Store) ListNotes(ctx context.Context, researchID uuid.UUID) ([]*OwnAppNote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, research_id, category, content, color, position_x, position_y, created_by, created_at, updated_at
		FROM own_app_notes WHERE research_id = $1
		ORDER BY created_at
	`, researchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*OwnAppNote
	for rows.Next() {
		var n OwnAppNote
		if err := rows.Scan(&n.ID, &n.ResearchID, &n.Category, &n.Content, &n.Color, &n.PositionX, &n.PositionY, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, &n)
	}

	return notes, nil
}

// ListNotesByCategory returns notes filtered by category
func (s *Store) ListNotesByCategory(ctx context.Context, researchID uuid.UUID, category NoteCategory) ([]*OwnAppNote, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, research_id, category, content, color, position_x, position_y, created_by, created_at, updated_at
		FROM own_app_notes WHERE research_id = $1 AND category = $2
		ORDER BY created_at
	`, researchID, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var notes []*OwnAppNote
	for rows.Next() {
		var n OwnAppNote
		if err := rows.Scan(&n.ID, &n.ResearchID, &n.Category, &n.Content, &n.Color, &n.PositionX, &n.PositionY, &n.CreatedBy, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		notes = append(notes, &n)
	}

	return notes, nil
}

// UpdateNote updates a note
func (s *Store) UpdateNote(ctx context.Context, note *OwnAppNote) error {
	result, err := s.pool.Exec(ctx, `
		UPDATE own_app_notes
		SET content = $2, color = $3, position_x = $4, position_y = $5, updated_at = $6
		WHERE id = $1
	`, note.ID, note.Content, note.Color, note.PositionX, note.PositionY, time.Now())

	if err != nil {
		return fmt.Errorf("failed to update note: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteNote deletes a note
func (s *Store) DeleteNote(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM own_app_notes WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete note: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- CompetitorApp Operations ---

// CreateCompetitor adds a competitor app to a research
func (s *Store) CreateCompetitor(ctx context.Context, competitor *CompetitorApp) error {
	screenshots, _ := json.Marshal(competitor.Screenshots)
	notes, _ := json.Marshal(competitor.Notes)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO competitor_apps (id, research_id, app_id, name, icon_url, screenshots, notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, competitor.ID, competitor.ResearchID, competitor.AppID, competitor.Name, competitor.IconURL, screenshots, notes, competitor.CreatedAt, competitor.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create competitor: %w", err)
	}
	return nil
}

// GetCompetitor retrieves a competitor by ID
func (s *Store) GetCompetitor(ctx context.Context, id uuid.UUID) (*CompetitorApp, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, research_id, app_id, name, icon_url, screenshots, notes, created_at, updated_at
		FROM competitor_apps WHERE id = $1
	`, id)

	var c CompetitorApp
	var screenshotsJSON, notesJSON []byte
	var name, iconURL *string

	err := row.Scan(&c.ID, &c.ResearchID, &c.AppID, &name, &iconURL, &screenshotsJSON, &notesJSON, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if name != nil {
		c.Name = *name
	}
	if iconURL != nil {
		c.IconURL = *iconURL
	}
	if screenshotsJSON != nil {
		_ = json.Unmarshal(screenshotsJSON, &c.Screenshots)
	}
	if notesJSON != nil {
		_ = json.Unmarshal(notesJSON, &c.Notes)
	}

	return &c, nil
}

// ListCompetitors returns all competitor apps for a research
func (s *Store) ListCompetitors(ctx context.Context, researchID uuid.UUID) ([]*CompetitorApp, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, research_id, app_id, name, icon_url, screenshots, notes, created_at, updated_at
		FROM competitor_apps WHERE research_id = $1
		ORDER BY created_at
	`, researchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var competitors []*CompetitorApp
	for rows.Next() {
		var c CompetitorApp
		var screenshotsJSON, notesJSON []byte
		var name, iconURL *string

		if err := rows.Scan(&c.ID, &c.ResearchID, &c.AppID, &name, &iconURL, &screenshotsJSON, &notesJSON, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}

		if name != nil {
			c.Name = *name
		}
		if iconURL != nil {
			c.IconURL = *iconURL
		}
		if screenshotsJSON != nil {
			_ = json.Unmarshal(screenshotsJSON, &c.Screenshots)
		}
		if notesJSON != nil {
			_ = json.Unmarshal(notesJSON, &c.Notes)
		}

		competitors = append(competitors, &c)
	}

	return competitors, nil
}

// UpdateCompetitor updates a competitor app
func (s *Store) UpdateCompetitor(ctx context.Context, competitor *CompetitorApp) error {
	screenshots, _ := json.Marshal(competitor.Screenshots)
	notes, _ := json.Marshal(competitor.Notes)

	result, err := s.pool.Exec(ctx, `
		UPDATE competitor_apps
		SET name = $2, icon_url = $3, screenshots = $4, notes = $5, updated_at = $6
		WHERE id = $1
	`, competitor.ID, competitor.Name, competitor.IconURL, screenshots, notes, time.Now())

	if err != nil {
		return fmt.Errorf("failed to update competitor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteCompetitor deletes a competitor app
func (s *Store) DeleteCompetitor(ctx context.Context, id uuid.UUID) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM competitor_apps WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("failed to delete competitor: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

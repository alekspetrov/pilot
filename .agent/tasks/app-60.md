# APP-60

**Created:** 2026-02-16

## Problem

Linear Issue APP-60: Own App Analysis

Add user's own app to research for side-by-side competitor comparison.

**Deliverables:**

* own_app_id column on researches
* Own app notes table (marketing/design)
* Own app input component (URL/package ID)
* Own app section with screenshots
* Sticky notes for keep/improve notes

**FigJam Reference:** Node 1:585 (Current Set Analysis)

**Effort:** \~14h

## Acceptance Criteria

- [x] `own_app_id` column on researches table
- [x] Own app notes table (marketing/design categories)
- [x] Own app input component API (URL/package ID fields)
- [x] Own app section with screenshots (JSON field)
- [x] Sticky notes with keep/improve categories

## Implementation

### Database Migration (`cloud/migrations/002_research_schema.sql`)

Created three tables:
- `researches`: Core research entity with `own_app_id`, `own_app_name`, `own_app_icon_url`, `own_app_screenshots`
- `own_app_notes`: Sticky notes with categories (marketing, design, keep, improve), color, position
- `competitor_apps`: Competitor apps for side-by-side comparison

### Go Package (`cloud/internal/research/`)

- `types.go`: Data models (Research, OwnAppNote, CompetitorApp) and input structs
- `store.go`: PostgreSQL CRUD operations with pgx
- `service.go`: Business logic layer
- `types_test.go`, `service_test.go`: Unit tests with mock store

### API Routes (`/orgs/{orgID}/researches/`)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/researches` | List all researches |
| POST | `/researches` | Create research |
| GET | `/researches/{id}` | Get research with details |
| PUT | `/researches/{id}` | Update research |
| DELETE | `/researches/{id}` | Delete research |
| PUT | `/researches/{id}/own-app` | Set own app details |
| GET/POST | `/researches/{id}/notes` | List/create notes |
| GET/PUT/DELETE | `/researches/{id}/notes/{noteID}` | Note CRUD |
| GET/POST | `/researches/{id}/competitors` | List/add competitors |
| GET/PUT/DELETE | `/researches/{id}/competitors/{id}` | Competitor CRUD |


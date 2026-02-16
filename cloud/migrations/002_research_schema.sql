-- Research schema for competitor and own app analysis
-- Migration: 002_research_schema.sql

-- Researches table: stores research sessions for competitor analysis
CREATE TABLE researches (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    own_app_id VARCHAR(512), -- App store URL or package ID (e.g., com.example.app)
    own_app_name VARCHAR(255),
    own_app_icon_url VARCHAR(512),
    own_app_screenshots JSONB DEFAULT '[]', -- Array of screenshot URLs
    settings JSONB DEFAULT '{}',
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_researches_org ON researches(org_id);
CREATE INDEX idx_researches_created_at ON researches(created_at DESC);

-- Own app notes table: sticky notes for keep/improve feedback
CREATE TABLE own_app_notes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    research_id UUID NOT NULL REFERENCES researches(id) ON DELETE CASCADE,
    category VARCHAR(50) NOT NULL, -- 'marketing', 'design', 'keep', 'improve'
    content TEXT NOT NULL,
    color VARCHAR(20) DEFAULT 'yellow', -- Note color for UI
    position_x INTEGER DEFAULT 0, -- Position for sticky note placement
    position_y INTEGER DEFAULT 0,
    created_by UUID NOT NULL REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_own_app_notes_research ON own_app_notes(research_id);
CREATE INDEX idx_own_app_notes_category ON own_app_notes(category);

-- Competitor apps table: apps being compared to own app
CREATE TABLE competitor_apps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    research_id UUID NOT NULL REFERENCES researches(id) ON DELETE CASCADE,
    app_id VARCHAR(512) NOT NULL, -- App store URL or package ID
    name VARCHAR(255),
    icon_url VARCHAR(512),
    screenshots JSONB DEFAULT '[]', -- Array of screenshot URLs
    notes JSONB DEFAULT '[]', -- Quick notes about this competitor
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_competitor_apps_research ON competitor_apps(research_id);

-- Add updated_at trigger for researches
CREATE TRIGGER update_researches_updated_at
    BEFORE UPDATE ON researches
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Add updated_at trigger for own_app_notes
CREATE TRIGGER update_own_app_notes_updated_at
    BEFORE UPDATE ON own_app_notes
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Add updated_at trigger for competitor_apps
CREATE TRIGGER update_competitor_apps_updated_at
    BEFORE UPDATE ON competitor_apps
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

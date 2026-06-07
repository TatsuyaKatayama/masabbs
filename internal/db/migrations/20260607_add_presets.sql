-- migration: add configs table and team_id to threads table (v1.3.0)

-- 1. Create configs table
CREATE TABLE IF NOT EXISTS configs (
    id TEXT PRIMARY KEY,               -- ULID
    name TEXT NOT NULL UNIQUE,         -- Unique human-readable name
    description TEXT,                  -- Optional description
    data JSONB NOT NULL,               -- Snapshot data (teams, agents, relations)
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- 2. Add team_id to threads table
ALTER TABLE threads ADD COLUMN IF NOT EXISTS team_id TEXT;
ALTER TABLE threads ADD CONSTRAINT fk_threads_team FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_threads_team_id ON threads(team_id);

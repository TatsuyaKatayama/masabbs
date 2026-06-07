-- Teams Table
CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY, -- ULID
    name TEXT NOT NULL,
    description TEXT,
    mission TEXT DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Agents Table
CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY, -- NATS NKey/AgentID
    name TEXT NOT NULL,
    role TEXT NOT NULL, -- manager / worker / observer
    mission TEXT DEFAULT '', -- Individual contribution to team mission
    tools JSONB DEFAULT '[]', -- List of tools/capabilities
    capabilities JSONB DEFAULT '[]',
    status TEXT DEFAULT 'offline', -- online / offline / busy
    team_id TEXT REFERENCES teams(id),
    ui_pos_x FLOAT DEFAULT 0, -- Admin UI position
    ui_pos_y FLOAT DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Agent Relations (Graph structure v1.2.0)
DROP TABLE IF EXISTS agent_relations;
CREATE TABLE agent_relations (
    id TEXT PRIMARY KEY, -- ULID
    team_id TEXT REFERENCES teams(id) ON DELETE CASCADE,
    source_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    target_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    source_handle TEXT, -- "t", "b", "l", "r"
    target_handle TEXT, -- "t", "b", "l", "r"
    relation_type TEXT NOT NULL, -- leader / subordinate / consultant / reviewer
    relation_category TEXT NOT NULL, -- vertical / horizontal
    CONSTRAINT unique_relation UNIQUE (source_id, target_id, relation_type)
);

-- Team Memberships (many-to-many)
CREATE TABLE IF NOT EXISTS team_agents (
    team_id TEXT REFERENCES teams(id) ON DELETE CASCADE,
    agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (team_id, agent_id)
);

-- Threads Table
CREATE TABLE IF NOT EXISTS threads (
    id TEXT PRIMARY KEY, -- ULID
    parent_thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
    created_by_agent TEXT REFERENCES agents(id) ON DELETE CASCADE,
    assigned_agent TEXT REFERENCES agents(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'open', -- open / assigned / collecting / processing / done / error
    team_id TEXT REFERENCES teams(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);


-- Tasks Table (History of messages/actions)
-- Includes 'observers' and other fields from the communication spec
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY, -- ULID
    thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
    agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE, -- The 'from' field
    type TEXT NOT NULL, -- task / offer / assign / result / status / event / shutdown
    to_agents TEXT[], -- Array of agent_ids
    observers TEXT[], -- Array of agent_ids
    payload JSONB NOT NULL, -- Structured payload defined in communication spec
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Logs Table for debugging and history
CREATE TABLE IF NOT EXISTS logs (
    id BIGSERIAL PRIMARY KEY,
    thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE, -- NULL allowed for thread-independent logs (e.g. agent startup)
    agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    level TEXT NOT NULL, -- info / warn / error
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_tasks_thread_id ON tasks(thread_id);
CREATE INDEX IF NOT EXISTS idx_tasks_agent_id ON tasks(agent_id);
CREATE INDEX IF NOT EXISTS idx_agents_team_id ON agents(team_id);
CREATE INDEX IF NOT EXISTS idx_team_agents_agent_id ON team_agents(agent_id);
CREATE INDEX IF NOT EXISTS idx_threads_parent_id ON threads(parent_thread_id);
CREATE INDEX IF NOT EXISTS idx_threads_status ON threads(status);
CREATE INDEX IF NOT EXISTS idx_logs_thread_id ON logs(thread_id);

-- Triggers for updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

DROP TRIGGER IF EXISTS update_teams_updated_at ON teams;
CREATE TRIGGER update_teams_updated_at
    BEFORE UPDATE ON teams
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_agents_updated_at ON agents;
CREATE TRIGGER update_agents_updated_at
    BEFORE UPDATE ON agents
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

DROP TRIGGER IF EXISTS update_threads_updated_at ON threads;
CREATE TRIGGER update_threads_updated_at
    BEFORE UPDATE ON threads
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Configs Table (v1.3.0)
CREATE TABLE IF NOT EXISTS configs (
    id TEXT PRIMARY KEY,               -- ULID
    name TEXT NOT NULL UNIQUE,         -- Unique human-readable name
    description TEXT,                  -- Optional description
    data JSONB NOT NULL,               -- Snapshot data (teams, agents, relations)
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

DROP TRIGGER IF EXISTS update_configs_updated_at ON configs;
CREATE TRIGGER update_configs_updated_at
    BEFORE UPDATE ON configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

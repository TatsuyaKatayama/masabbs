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
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Agent Relations (Hierarchical structure)
CREATE TABLE IF NOT EXISTS agent_relations (
    parent_agent_id TEXT REFERENCES agents(id),
    child_agent_id TEXT REFERENCES agents(id),
    relation_type TEXT NOT NULL DEFAULT 'manages',
    PRIMARY KEY (parent_agent_id, child_agent_id)
);

-- Threads Table
CREATE TABLE IF NOT EXISTS threads (
    id TEXT PRIMARY KEY, -- ULID
    parent_thread_id TEXT REFERENCES threads(id),
    created_by_agent TEXT REFERENCES agents(id),
    assigned_agent TEXT REFERENCES agents(id),
    status TEXT NOT NULL DEFAULT 'open', -- open / assigned / collecting / processing / done / error
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Tasks Table (History of messages/actions)
-- Includes 'observers' and other fields from the communication spec
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY, -- ULID
    thread_id TEXT REFERENCES threads(id),
    agent_id TEXT REFERENCES agents(id), -- The 'from' field
    type TEXT NOT NULL, -- task / offer / assign / result / status / event / shutdown
    to_agents TEXT[], -- Array of agent_ids
    observers TEXT[], -- Array of agent_ids
    payload JSONB NOT NULL, -- Structured payload defined in communication spec
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Logs Table for debugging and history
CREATE TABLE IF NOT EXISTS logs (
    id BIGSERIAL PRIMARY KEY,
    thread_id TEXT REFERENCES threads(id), -- NULL allowed for thread-independent logs (e.g. agent startup)
    agent_id TEXT REFERENCES agents(id),
    level TEXT NOT NULL, -- info / warn / error
    message TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_tasks_thread_id ON tasks(thread_id);
CREATE INDEX IF NOT EXISTS idx_tasks_agent_id ON tasks(agent_id);
CREATE INDEX IF NOT EXISTS idx_agents_team_id ON agents(team_id);
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

-- Thread Reflection Requests Table (Step 6)
CREATE TABLE IF NOT EXISTS thread_reflection_requests (
    id TEXT PRIMARY KEY,                       -- ULID
    thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
    reflection_thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
    requested_by_agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending',   -- pending / completed
    due_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Thread Reflections Table (Step 6)
CREATE TABLE IF NOT EXISTS thread_reflections (
    id TEXT PRIMARY KEY,                       -- ULID
    request_id TEXT REFERENCES thread_reflection_requests(id) ON DELETE CASCADE,
    thread_id TEXT REFERENCES threads(id) ON DELETE CASCADE,
    team_id TEXT REFERENCES teams(id) ON DELETE CASCADE,
    from_agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    target_agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL,
    score INTEGER NOT NULL,                    -- -1 / 0 / 1
    reason TEXT NOT NULL,
    suggestion TEXT,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT unique_reflection UNIQUE (request_id, from_agent_id, target_agent_id, dimension)
);

CREATE INDEX IF NOT EXISTS idx_thread_reflection_requests_thread_id ON thread_reflection_requests(thread_id);
CREATE INDEX IF NOT EXISTS idx_thread_reflections_request_id ON thread_reflections(request_id);
CREATE INDEX IF NOT EXISTS idx_thread_reflections_thread_id ON thread_reflections(thread_id);
CREATE INDEX IF NOT EXISTS idx_thread_reflections_from_agent ON thread_reflections(from_agent_id);
CREATE INDEX IF NOT EXISTS idx_thread_reflections_target_agent ON thread_reflections(target_agent_id);

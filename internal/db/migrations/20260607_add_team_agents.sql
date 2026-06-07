-- migration: add many-to-many team memberships

CREATE TABLE IF NOT EXISTS team_agents (
    team_id TEXT REFERENCES teams(id) ON DELETE CASCADE,
    agent_id TEXT REFERENCES agents(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (team_id, agent_id)
);

INSERT INTO team_agents (team_id, agent_id)
SELECT team_id, id
FROM agents
WHERE team_id IS NOT NULL
ON CONFLICT DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_team_agents_agent_id ON team_agents(agent_id);


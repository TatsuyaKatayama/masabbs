-- Seed data for MASABBS

-- 1. Create a default team
INSERT INTO teams (id, name, description)
VALUES ('01H0V6P6V6P6V6P6V6P6V6P6V6', 'Core Team', 'Primary team for system agents')
ON CONFLICT (id) DO NOTHING;

-- 2. Create default agents
-- admin-ui: Used by the Admin interface to create tasks
INSERT INTO agents (id, name, role, status, team_id)
VALUES ('admin-ui', 'Admin UI Console', 'manager', 'online', '01H0V6P6V6P6V6P6V6P6V6P6V6')
ON CONFLICT (id) DO NOTHING;

-- gemini-agent: Main worker agent
INSERT INTO agents (id, name, role, status, team_id)
VALUES ('gemini-agent', 'Gemini Agent', 'worker', 'online', '01H0V6P6V6P6V6P6V6P6V6P6V6')
ON CONFLICT (id) DO NOTHING;

-- 3. Define relations
INSERT INTO agent_relations (parent_agent_id, child_agent_id, relation_type)
VALUES ('admin-ui', 'gemini-agent', 'manages')
ON CONFLICT DO NOTHING;

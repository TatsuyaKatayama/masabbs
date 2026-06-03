-- Seed data for MASABBS (v1.2.0)

-- 1. Create a default team
INSERT INTO teams (id, name, description, mission)
VALUES ('01H0V6P6V6P6V6P6V6P6V6P6V6', 'Core Team', 'Primary team for system agents', 'システム全体の運用とエージェントの監視を行う')
ON CONFLICT (id) DO NOTHING;

-- 2. Create default agents
-- admin-ui: Used by the Admin interface to create tasks
INSERT INTO agents (id, name, role, status, team_id, ui_pos_x, ui_pos_y)
VALUES ('admin-ui', 'Admin UI Console', 'manager', 'online', '01H0V6P6V6P6V6P6V6P6V6P6V6', 100, 100)
ON CONFLICT (id) DO NOTHING;

-- gemini-agent: Main worker agent
INSERT INTO agents (id, name, role, status, team_id, ui_pos_x, ui_pos_y)
VALUES ('gemini-agent', 'Gemini Agent', 'worker', 'online', '01H0V6P6V6P6V6P6V6P6V6P6V6', 400, 100)
ON CONFLICT (id) DO NOTHING;

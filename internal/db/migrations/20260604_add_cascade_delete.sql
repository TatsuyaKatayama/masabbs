-- migration: add cascade delete to existing tables

-- 2. threads
ALTER TABLE threads DROP CONSTRAINT IF EXISTS threads_parent_thread_id_fkey;
ALTER TABLE threads ADD CONSTRAINT threads_parent_thread_id_fkey FOREIGN KEY (parent_thread_id) REFERENCES threads(id) ON DELETE CASCADE;

ALTER TABLE threads DROP CONSTRAINT IF EXISTS threads_created_by_agent_fkey;
ALTER TABLE threads ADD CONSTRAINT threads_created_by_agent_fkey FOREIGN KEY (created_by_agent) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE threads DROP CONSTRAINT IF EXISTS threads_assigned_agent_fkey;
ALTER TABLE threads ADD CONSTRAINT threads_assigned_agent_fkey FOREIGN KEY (assigned_agent) REFERENCES agents(id) ON DELETE SET NULL;

-- 1. tasks
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_thread_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_thread_id_fkey FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE;

ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_agent_id_fkey;
ALTER TABLE tasks ADD CONSTRAINT tasks_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

-- 3. logs
ALTER TABLE logs DROP CONSTRAINT IF EXISTS logs_thread_id_fkey;
ALTER TABLE logs ADD CONSTRAINT logs_thread_id_fkey FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE;

ALTER TABLE logs DROP CONSTRAINT IF EXISTS logs_agent_id_fkey;
ALTER TABLE logs ADD CONSTRAINT logs_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

package api

import (
	"context"
	"fmt"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5"
)

func replaceConfigurationSnapshot(ctx context.Context, tx pgx.Tx, snap *models.ConfigurationSnapshot) error {
	if _, err := tx.Exec(ctx, "DELETE FROM logs"); err != nil {
		return fmt.Errorf("failed to clear logs: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM tasks"); err != nil {
		return fmt.Errorf("failed to clear tasks: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM threads"); err != nil {
		return fmt.Errorf("failed to clear threads: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM agent_relations"); err != nil {
		return fmt.Errorf("failed to clear agent relations: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM team_agents"); err != nil {
		return fmt.Errorf("failed to clear team agents: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM agents"); err != nil {
		return fmt.Errorf("failed to clear agents: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM teams"); err != nil {
		return fmt.Errorf("failed to clear teams: %w", err)
	}

	for _, team := range snap.Teams {
		if _, err := tx.Exec(ctx, `
			INSERT INTO teams (id, name, description, mission, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, team.ID, team.Name, team.Description, team.Mission, team.CreatedAt, team.UpdatedAt); err != nil {
			return fmt.Errorf("failed to insert team %s: %w", team.ID, err)
		}
	}

	for _, agent := range snap.Agents {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agents (id, name, role, mission, tools, capabilities, status, team_id, ui_pos_x, ui_pos_y, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		`, agent.ID, agent.Name, agent.Role, agent.Mission, agent.Tools, agent.Capabilities, agent.Status, agent.TeamID, agent.UIPosX, agent.UIPosY, agent.CreatedAt, agent.UpdatedAt); err != nil {
			return fmt.Errorf("failed to insert agent %s: %w", agent.ID, err)
		}
	}

	teamAgents := snap.TeamAgents
	if len(teamAgents) == 0 {
		for _, agent := range snap.Agents {
			if agent.TeamID != nil {
				teamAgents = append(teamAgents, models.TeamAgent{TeamID: *agent.TeamID, AgentID: agent.ID, CreatedAt: agent.CreatedAt})
			}
		}
	}

	for _, teamAgent := range teamAgents {
		if _, err := tx.Exec(ctx, `
			INSERT INTO team_agents (team_id, agent_id, created_at)
			VALUES ($1, $2, $3)
		`, teamAgent.TeamID, teamAgent.AgentID, teamAgent.CreatedAt); err != nil {
			return fmt.Errorf("failed to insert team agent %s/%s: %w", teamAgent.TeamID, teamAgent.AgentID, err)
		}
	}

	for _, relation := range snap.Relations {
		if _, err := tx.Exec(ctx, `
			INSERT INTO agent_relations (id, team_id, source_id, target_id, source_handle, target_handle, relation_type, relation_category)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, relation.ID, relation.TeamID, relation.SourceID, relation.TargetID, relation.SourceHandle, relation.TargetHandle, relation.RelationType, relation.RelationCategory); err != nil {
			return fmt.Errorf("failed to insert agent relation %s: %w", relation.ID, err)
		}
	}

	return nil
}

func replaceThreadSnapshot(ctx context.Context, tx pgx.Tx, snap *models.ThreadSnapshot) error {
	if _, err := tx.Exec(ctx, "DELETE FROM logs"); err != nil {
		return fmt.Errorf("failed to clear logs: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM tasks"); err != nil {
		return fmt.Errorf("failed to clear tasks: %w", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM threads"); err != nil {
		return fmt.Errorf("failed to clear threads: %w", err)
	}

	for _, thread := range snap.Threads {
		if _, err := tx.Exec(ctx, `
			INSERT INTO threads (id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at)
			VALUES ($1, NULL, $2, $3, $4, $5, $6, $7)
		`, thread.ID, thread.CreatedByAgent, thread.AssignedAgent, thread.Status, thread.TeamID, thread.CreatedAt, thread.UpdatedAt); err != nil {
			return fmt.Errorf("failed to insert thread %s: %w", thread.ID, err)
		}
	}

	for _, thread := range snap.Threads {
		if thread.ParentThreadID == nil {
			continue
		}
		if _, err := tx.Exec(ctx, "UPDATE threads SET parent_thread_id = $1 WHERE id = $2", thread.ParentThreadID, thread.ID); err != nil {
			return fmt.Errorf("failed to restore parent for thread %s: %w", thread.ID, err)
		}
	}

	for _, task := range snap.Tasks {
		if _, err := tx.Exec(ctx, `
			INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, task.ID, task.ThreadID, task.AgentID, task.Type, task.ToAgents, task.Observers, task.Payload, task.CreatedAt); err != nil {
			return fmt.Errorf("failed to insert task %s: %w", task.ID, err)
		}
	}

	for _, log := range snap.Logs {
		if log.ID > 0 {
			if _, err := tx.Exec(ctx, `
				INSERT INTO logs (id, thread_id, agent_id, level, message, created_at)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, log.ID, log.ThreadID, log.AgentID, log.Level, log.Message, log.CreatedAt); err != nil {
				return fmt.Errorf("failed to insert log %d: %w", log.ID, err)
			}
			continue
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO logs (thread_id, agent_id, level, message, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, log.ThreadID, log.AgentID, log.Level, log.Message, log.CreatedAt); err != nil {
			return fmt.Errorf("failed to insert log: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		SELECT setval(
			pg_get_serial_sequence('logs', 'id'),
			COALESCE((SELECT MAX(id) FROM logs), 1),
			(SELECT COUNT(*) FROM logs) > 0
		)
	`); err != nil {
		return fmt.Errorf("failed to reset logs sequence: %w", err)
	}

	return nil
}

func replaceSavedConfigs(ctx context.Context, tx pgx.Tx, configs []models.Config) error {
	if _, err := tx.Exec(ctx, "DELETE FROM configs"); err != nil {
		return fmt.Errorf("failed to clear saved configs: %w", err)
	}

	for _, config := range configs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO configs (id, name, description, data, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, config.ID, config.Name, config.Description, config.Data, config.CreatedAt, config.UpdatedAt); err != nil {
			return fmt.Errorf("failed to insert saved config %s: %w", config.ID, err)
		}
	}

	return nil
}

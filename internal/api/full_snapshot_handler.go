package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/labstack/echo/v4"
)

// ExportSnapshot exports the full restorable state: configuration plus thread history.
func (h *Handler) ExportSnapshot(c echo.Context) error {
	ctx := c.Request().Context()

	config, err := h.captureCurrentSnapshot(ctx)
	if err != nil {
		c.Logger().Errorf("failed to capture config snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to capture configuration"})
	}

	threadSnapshot, err := h.captureThreadSnapshot(ctx)
	if err != nil {
		c.Logger().Errorf("failed to capture thread snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to capture threads"})
	}

	configs, err := h.captureSavedConfigs(ctx)
	if err != nil {
		c.Logger().Errorf("failed to capture saved configs: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to capture saved configs"})
	}

	snapshot := models.FullSnapshot{
		Teams:      config.Teams,
		Agents:     config.Agents,
		TeamAgents: config.TeamAgents,
		Relations:  config.Relations,
		Threads:    threadSnapshot.Threads,
		Tasks:      threadSnapshot.Tasks,
		Logs:       threadSnapshot.Logs,
		Configs:    configs,
	}

	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=masabbs-full-%d.json", time.Now().Unix()))
	return c.JSON(http.StatusOK, snapshot)
}

// ImportSnapshot restores the full state by replacing configuration and thread history.
func (h *Handler) ImportSnapshot(c echo.Context) error {
	ctx := c.Request().Context()

	var snapshot models.FullSnapshot
	if err := json.NewDecoder(c.Request().Body).Decode(&snapshot); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot start transaction"})
	}
	defer tx.Rollback(ctx)

	config := models.ConfigurationSnapshot{
		Teams:      snapshot.Teams,
		Agents:     snapshot.Agents,
		TeamAgents: snapshot.TeamAgents,
		Relations:  snapshot.Relations,
	}
	if err := replaceConfigurationSnapshot(ctx, tx, &config); err != nil {
		c.Logger().Errorf("failed to replace configuration snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to import configuration: %v", err)})
	}

	threads := models.ThreadSnapshot{
		Threads: snapshot.Threads,
		Tasks:   snapshot.Tasks,
		Logs:    snapshot.Logs,
	}
	if err := replaceThreadSnapshot(ctx, tx, &threads); err != nil {
		c.Logger().Errorf("failed to replace thread snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to import threads: %v", err)})
	}

	if err := replaceSavedConfigs(ctx, tx, snapshot.Configs); err != nil {
		c.Logger().Errorf("failed to replace saved configs: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to import saved configs: %v", err)})
	}

	if err := tx.Commit(ctx); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "transaction commit failed"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "full snapshot imported successfully"})
}

func (h *Handler) captureThreadSnapshot(ctx context.Context) (*models.ThreadSnapshot, error) {
	threadRows, err := h.DB.Query(ctx, `
		SELECT id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at
		FROM threads
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query threads: %w", err)
	}
	defer threadRows.Close()

	var threads []models.Thread
	for threadRows.Next() {
		var thread models.Thread
		if err := threadRows.Scan(&thread.ID, &thread.ParentThreadID, &thread.CreatedByAgent, &thread.AssignedAgent, &thread.Status, &thread.TeamID, &thread.CreatedAt, &thread.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan thread: %w", err)
		}
		threads = append(threads, thread)
	}
	if err := threadRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate threads: %w", err)
	}

	taskRows, err := h.DB.Query(ctx, `
		SELECT id, thread_id, agent_id, type, to_agents, observers, payload, created_at
		FROM tasks
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query tasks: %w", err)
	}
	defer taskRows.Close()

	var tasks []models.Task
	for taskRows.Next() {
		var task models.Task
		if err := taskRows.Scan(&task.ID, &task.ThreadID, &task.AgentID, &task.Type, &task.ToAgents, &task.Observers, &task.Payload, &task.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := taskRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate tasks: %w", err)
	}

	logRows, err := h.DB.Query(ctx, `
		SELECT id, thread_id, agent_id, level, message, created_at
		FROM logs
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query logs: %w", err)
	}
	defer logRows.Close()

	var logs []models.TaskLog
	for logRows.Next() {
		var log models.TaskLog
		if err := logRows.Scan(&log.ID, &log.ThreadID, &log.AgentID, &log.Level, &log.Message, &log.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		logs = append(logs, log)
	}
	if err := logRows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate logs: %w", err)
	}

	return &models.ThreadSnapshot{Threads: threads, Tasks: tasks, Logs: logs}, nil
}

func (h *Handler) captureSavedConfigs(ctx context.Context) ([]models.Config, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT id, name, description, data, created_at, updated_at
		FROM configs
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query saved configs: %w", err)
	}
	defer rows.Close()

	var configs []models.Config
	for rows.Next() {
		var config models.Config
		if err := rows.Scan(&config.ID, &config.Name, &config.Description, &config.Data, &config.CreatedAt, &config.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan saved config: %w", err)
		}
		configs = append(configs, config)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate saved configs: %w", err)
	}

	return configs, nil
}

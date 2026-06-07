package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ThreadSnapshotHandler struct {
	DB *pgxpool.Pool
}

// RegisterThreadSnapshotRoutes registers the export/import endpoints for threads and their messages.
func RegisterThreadSnapshotRoutes(e *echo.Echo, db *pgxpool.Pool) {
	h := &ThreadSnapshotHandler{DB: db}
	api := e.Group("/api/v1")
	api.GET("/threads/export", h.ExportThreads)
	api.POST("/threads/import", h.ImportThreads)
}

// ExportThreads returns a snapshot that contains all threads, messages, and logs.
func (h *ThreadSnapshotHandler) ExportThreads(c echo.Context) error {
	ctx := c.Request().Context()

	// ---- fetch threads -----------------------------------------------------
	threadRows, err := h.DB.Query(ctx, `
        SELECT id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at
        FROM threads
    `)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query threads"})
	}
	defer threadRows.Close()

	var threads []models.Thread
	for threadRows.Next() {
		var t models.Thread
		if err := threadRows.Scan(&t.ID, &t.ParentThreadID, &t.CreatedByAgent, &t.AssignedAgent, &t.Status, &t.TeamID, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		threads = append(threads, t)
	}

	// ---- fetch tasks (messages) -------------------------------------------
	taskRows, err := h.DB.Query(ctx, `
		SELECT id, thread_id, agent_id, type, to_agents, observers, payload, created_at
		FROM tasks
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query tasks"})
	}
	defer taskRows.Close()

	var tasks []models.Task
	for taskRows.Next() {
		var tk models.Task
		if err := taskRows.Scan(&tk.ID, &tk.ThreadID, &tk.AgentID, &tk.Type, &tk.ToAgents, &tk.Observers, &tk.Payload, &tk.CreatedAt); err != nil {
			continue
		}
		tasks = append(tasks, tk)
	}

	logRows, err := h.DB.Query(ctx, `
		SELECT id, thread_id, agent_id, level, message, created_at
		FROM logs
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query logs"})
	}
	defer logRows.Close()

	var logs []models.TaskLog
	for logRows.Next() {
		var log models.TaskLog
		if err := logRows.Scan(&log.ID, &log.ThreadID, &log.AgentID, &log.Level, &log.Message, &log.CreatedAt); err != nil {
			continue
		}
		logs = append(logs, log)
	}

	snapshot := models.ThreadSnapshot{Threads: threads, Tasks: tasks, Logs: logs}
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=masabbs-threads-%d.json", time.Now().Unix()))
	return c.JSON(http.StatusOK, snapshot)
}

// ImportThreads receives a snapshot of threads, messages, and logs and replaces the DB state.
func (h *ThreadSnapshotHandler) ImportThreads(c echo.Context) error {
	ctx := c.Request().Context()
	var payload models.ThreadSnapshot
	if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
	}

	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot start transaction"})
	}
	defer tx.Rollback(ctx)

	if err := replaceThreadSnapshot(ctx, tx, &payload); err != nil {
		c.Logger().Errorf("failed to replace thread snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to import threads: %v", err)})
	}

	if err = tx.Commit(ctx); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "transaction commit failed"})
	}
	return c.JSON(http.StatusOK, map[string]string{"message": "threads, messages, and logs imported successfully"})
}

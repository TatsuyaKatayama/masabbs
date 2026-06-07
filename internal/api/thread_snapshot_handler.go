package api

import (
    "encoding/json"
    "net/http"

    "github.com/TatsuyaKatayama/masabbs/internal/models"
    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/labstack/echo/v4"
    "github.com/oklog/ulid/v2"
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

// ExportThreads returns a snapshot that contains all threads and their tasks (messages).
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
        SELECT id, thread_id, agent_id, type, payload, created_at
        FROM tasks
    `)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query tasks"})
    }
    defer taskRows.Close()

    var tasks []models.Task
    for taskRows.Next() {
        var tk models.Task
        if err := taskRows.Scan(&tk.ID, &tk.ThreadID, &tk.AgentID, &tk.Type, &tk.Payload, &tk.CreatedAt); err != nil {
            continue
        }
        tasks = append(tasks, tk)
    }

    snapshot := struct {
        Threads []models.Thread `json:"threads"`
        Tasks   []models.Task   `json:"tasks"`
    }{Threads: threads, Tasks: tasks}
    return c.JSON(http.StatusOK, snapshot)
}

// ImportThreads receives a snapshot of threads and tasks and upserts them into the DB.
func (h *ThreadSnapshotHandler) ImportThreads(c echo.Context) error {
    ctx := c.Request().Context()
    var payload struct {
        Threads []models.Thread `json:"threads"`
        Tasks   []models.Task   `json:"tasks"`
    }
    if err := json.NewDecoder(c.Request().Body).Decode(&payload); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
    }

    tx, err := h.DB.Begin(ctx)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot start transaction"})
    }
    defer tx.Rollback(ctx)

    // Upsert threads
    for _, th := range payload.Threads {
        _, err = tx.Exec(ctx, `
            INSERT INTO threads (id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
            ON CONFLICT (id) DO UPDATE SET
                parent_thread_id = EXCLUDED.parent_thread_id,
                created_by_agent = EXCLUDED.created_by_agent,
                assigned_agent = EXCLUDED.assigned_agent,
                status = EXCLUDED.status,
                team_id = EXCLUDED.team_id,
                created_at = EXCLUDED.created_at,
                updated_at = EXCLUDED.updated_at
        `, th.ID, th.ParentThreadID, th.CreatedByAgent, th.AssignedAgent, th.Status, th.TeamID, th.CreatedAt, th.UpdatedAt)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to upsert thread"})
        }
    }

    // Upsert tasks (messages)
    for _, tk := range payload.Tasks {
        id := tk.ID
        if id == "" {
            id = ulid.Make().String()
        }
        _, err = tx.Exec(ctx, `
            INSERT INTO tasks (id, thread_id, agent_id, type, payload, created_at)
            VALUES ($1, $2, $3, $4, $5, $6)
            ON CONFLICT (id) DO UPDATE SET
                thread_id = EXCLUDED.thread_id,
                agent_id = EXCLUDED.agent_id,
                type = EXCLUDED.type,
                payload = EXCLUDED.payload,
                created_at = EXCLUDED.created_at
        `, id, tk.ThreadID, tk.AgentID, tk.Type, tk.Payload, tk.CreatedAt)
        if err != nil {
            return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to upsert task"})
        }
    }

    if err = tx.Commit(ctx); err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "transaction commit failed"})
    }
    return c.JSON(http.StatusOK, map[string]string{"message": "threads and messages imported successfully"})
}

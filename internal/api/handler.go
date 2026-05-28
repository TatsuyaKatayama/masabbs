package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/nats"
	"github.com/TatsuyaKatayama/masabbs/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/oklog/ulid/v2"
)

type Handler struct {
	DB           *pgxpool.Pool
	NATS         *nats.Client
	Storage      storage.StorageProvider
	AuthProvider *auth.Provider
}

// RegisterRoutes sets up all API endpoints
func RegisterRoutes(e *echo.Echo, db *pgxpool.Pool, nc *nats.Client, sc storage.StorageProvider, hub *Hub, authProvider *auth.Provider) {
	h := &Handler{
		DB:           db,
		NATS:         nc,
		Storage:      sc,
		AuthProvider: authProvider,
	}
	api := e.Group("/api/v1")
	api.POST("/threads", h.CreateThread)
	api.GET("/threads", h.GetThreads)
	api.GET("/threads/:id/tasks", h.GetThreadTasks)
	api.DELETE("/threads/:id", h.DeleteThread)
	api.GET("/agents", h.GetAgents)
	api.POST("/agents", h.CreateAgent)
	api.GET("/agents/:id", h.GetAgent)
	api.PATCH("/agents/:id", h.UpdateAgent)
	api.POST("/agents/:id/credentials", h.GenerateCredentials)
	api.GET("/tasks", h.GetTasks)
	api.GET("/storage/files", h.ListS3Files)
	api.GET("/storage/presign", h.GetS3PresignedURL)

	api.GET("/teams", h.GetTeams)
	api.PATCH("/teams/:id", h.UpdateTeam)

	// WebSocket for Admin UI

	e.GET("/ws", func(c echo.Context) error {
		hub.ServeWS(c.Response(), c.Request())
		return nil
	})
}

type CreateThreadRequest struct {
	ThreadID       *string  `json:"thread_id,omitempty"`
	Command        string   `json:"command"`
	CreatedByAgent string   `json:"created_by_agent"`
	To             []string `json:"to,omitempty"`
	Observers      []string `json:"observers,omitempty"`
	ParentThreadID *string  `json:"parent_thread_id,omitempty"`
	Deadline       string   `json:"deadline"`
}

type CreateThreadResponse struct {
	ThreadID string `json:"thread_id"`
	InputDir string `json:"input_dir"`
}

func (h *Handler) CreateThread(c echo.Context) error {
	ctx := c.Request().Context()
	var err error
	var req CreateThreadRequest
	if err = c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.Command == "" || req.CreatedByAgent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command and created_by_agent are required"})
	}

	var threadID string
	var inputDir string
	isExisting := false

	// 1. Determine Thread ID
	if req.ThreadID != nil && *req.ThreadID != "" {
		threadID = *req.ThreadID
		// Check if thread exists
		var exists bool
		err = h.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM threads WHERE id = $1)", threadID).Scan(&exists)
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database check failed"})
		}
		if !exists {
			return c.JSON(http.StatusNotFound, map[string]string{"error": "specified thread_id not found"})
		}
		isExisting = true
		inputDir = h.Storage.GetThreadInputPath(threadID)
	} else {
		// Generate new ULID
		threadID = ulid.Make().String()
		inputDir = h.Storage.GetThreadInputPath(threadID)

		// 2. Create S3 Folders
		if err = h.Storage.CreateThreadFolders(ctx, threadID); err != nil {
			c.Logger().Errorf("failed to create s3 folders: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
		}

		// 3. Create DB record
		_, err = h.DB.Exec(ctx, `
			INSERT INTO threads (id, parent_thread_id, created_by_agent, status)
			VALUES ($1, $2, $3, 'open')
		`, threadID, req.ParentThreadID, req.CreatedByAgent)
		if err != nil {
			c.Logger().Errorf("failed to insert thread: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database record creation failed"})
		}
	}

	// 4. Publish to NATS
	taskPayload := models.TaskPayload{
		Command:  req.Command,
		InputDir: inputDir,
		Deadline: req.Deadline,
	}
	var payloadBytes []byte
	payloadBytes, err = json.Marshal(taskPayload)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal encoding error"})
	}

	envelope := models.MessageEnvelope{
		Type:      "task",
		ThreadID:  &threadID,
		From:      req.CreatedByAgent,
		To:        req.To,
		Observers: req.Observers,
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	}
	var envelopeBytes []byte
	envelopeBytes, err = json.Marshal(envelope)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal encoding error"})
	}

	subject := fmt.Sprintf("board.task.%s", threadID)
	if _, err = h.NATS.JS.Publish(ctx, subject, envelopeBytes); err != nil {
		c.Logger().Errorf("failed to publish to nats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "messaging error"})
	}

	if isExisting {
		// Update updated_at for sorting
		h.DB.Exec(ctx, "UPDATE threads SET updated_at = CURRENT_TIMESTAMP WHERE id = $1", threadID)
	}

	return c.JSON(http.StatusCreated, CreateThreadResponse{
		ThreadID: threadID,
		InputDir: inputDir,
	})
}

func (h *Handler) GenerateCredentials(c echo.Context) error {
	agentID := c.Param("id")
	if agentID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent id is required"})
	}

	// Verify agent exists in DB and get its role
	var role string
	err := h.DB.QueryRow(c.Request().Context(), "SELECT role FROM agents WHERE id = $1", agentID).Scan(&role)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	creds, err := h.AuthProvider.GenerateAgentCredentials(agentID, role)
	if err != nil {
		c.Logger().Errorf("failed to generate credentials: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to generate credentials"})
	}

	return c.JSON(http.StatusCreated, creds)
}

func (h *Handler) GetTasks(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, payload, type, agent_id, thread_id, to_agents, observers, created_at 
		FROM tasks 
		ORDER BY created_at DESC 
		LIMIT 100
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	tasks := []models.MessageEnvelope{}
	for rows.Next() {
		var id string
		var payload []byte
		var msgType, agentID string
		var threadID *string
		var toAgents, observers []string
		var createdAt time.Time
		if err := rows.Scan(&id, &payload, &msgType, &agentID, &threadID, &toAgents, &observers, &createdAt); err != nil {
			c.Logger().Errorf("scan error: %v", err)
			continue
		}

		tasks = append(tasks, models.MessageEnvelope{
			ID:        id,
			Type:      msgType,
			ThreadID:  threadID,
			From:      agentID,
			To:        toAgents,
			Observers: observers,
			Timestamp: createdAt.Unix(),
			Payload:   payload,
		})
	}

	return c.JSON(http.StatusOK, tasks)
}

type CreateAgentRequest struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Role    string  `json:"role"`
	Mission string  `json:"mission,omitempty"`
	TeamID  *string `json:"team_id,omitempty"`
}

func (h *Handler) CreateAgent(c echo.Context) error {
	var req CreateAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.ID == "" || req.Name == "" || req.Role == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id, name, and role are required"})
	}

	_, err := h.DB.Exec(c.Request().Context(), `
		INSERT INTO agents (id, name, role, mission, status, team_id)
		VALUES ($1, $2, $3, $4, 'offline', $5)
	`, req.ID, req.Name, req.Role, req.Mission, req.TeamID)
	if err != nil {
		c.Logger().Errorf("failed to create agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create agent"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"message": "agent created successfully"})
}

func (h *Handler) GetAgents(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, name, role, mission, status, team_id, created_at, updated_at, tools, capabilities
		FROM agents
		ORDER BY name ASC
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	agents := []models.Agent{}
	for rows.Next() {
		var a models.Agent
		err := rows.Scan(
			&a.ID, &a.Name, &a.Role, &a.Mission, &a.Status, &a.TeamID,
			&a.CreatedAt, &a.UpdatedAt, &a.Tools, &a.Capabilities,
		)
		if err != nil {
			continue
		}
		agents = append(agents, a)
	}

	return c.JSON(http.StatusOK, agents)
}

func (h *Handler) GetAgent(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent id is required"})
	}

	var a models.Agent
	var teamMission *string
	err := h.DB.QueryRow(c.Request().Context(), `
		SELECT a.id, a.name, a.role, a.mission, a.status, a.team_id, a.created_at, a.updated_at, a.tools, a.capabilities, t.mission
		FROM agents a
		LEFT JOIN teams t ON a.team_id = t.id
		WHERE a.id = $1
	`, id).Scan(
		&a.ID, &a.Name, &a.Role, &a.Mission, &a.Status, &a.TeamID,
		&a.CreatedAt, &a.UpdatedAt, &a.Tools, &a.Capabilities, &teamMission,
	)

	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	// We can return a custom response or just the agent.
	// Let's return a map to include team mission easily.
	resp := map[string]interface{}{
		"agent":        a,
		"team_mission": teamMission,
	}

	return c.JSON(http.StatusOK, resp)
}

type UpdateAgentRequest struct {
	Name    *string `json:"name,omitempty"`
	Role    *string `json:"role,omitempty"`
	Mission *string `json:"mission,omitempty"`
	TeamID  *string `json:"team_id,omitempty"`
}

func (h *Handler) UpdateAgent(c echo.Context) error {
	id := c.Param("id")
	var req UpdateAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	// Dynamic update
	query := "UPDATE agents SET updated_at = CURRENT_TIMESTAMP"
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argIdx)
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Role != nil {
		query += fmt.Sprintf(", role = $%d", argIdx)
		args = append(args, *req.Role)
		argIdx++
	}
	if req.Mission != nil {
		query += fmt.Sprintf(", mission = $%d", argIdx)
		args = append(args, *req.Mission)
		argIdx++
	}
	if req.TeamID != nil {
		query += fmt.Sprintf(", team_id = $%d", argIdx)
		args = append(args, *req.TeamID)
		argIdx++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	_, err := h.DB.Exec(c.Request().Context(), query, args...)
	if err != nil {
		c.Logger().Errorf("failed to update agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update agent"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "agent updated successfully"})
}

type UpdateTeamRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Mission     *string `json:"mission,omitempty"`
}
func (h *Handler) GetTeams(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, name, description, mission, created_at, updated_at
		FROM teams
		ORDER BY name ASC
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	teams := []models.Team{}
	for rows.Next() {
		var t models.Team
		err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Mission, &t.CreatedAt, &t.UpdatedAt)
		if err != nil {
			continue
		}
		teams = append(teams, t)
	}

	return c.JSON(http.StatusOK, teams)
}

func (h *Handler) UpdateTeam(c echo.Context) error {
	id := c.Param("id")
	var req UpdateTeamRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	query := "UPDATE teams SET updated_at = CURRENT_TIMESTAMP"
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		query += fmt.Sprintf(", name = $%d", argIdx)
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Description != nil {
		query += fmt.Sprintf(", description = $%d", argIdx)
		args = append(args, *req.Description)
		argIdx++
	}
	if req.Mission != nil {
		query += fmt.Sprintf(", mission = $%d", argIdx)
		args = append(args, *req.Mission)
		argIdx++
	}

	query += fmt.Sprintf(" WHERE id = $%d", argIdx)
	args = append(args, id)

	_, err := h.DB.Exec(c.Request().Context(), query, args...)
	if err != nil {
		c.Logger().Errorf("failed to update team: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to update team"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "team updated successfully"})
}

func (h *Handler) GetThreads(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, parent_thread_id, created_by_agent, assigned_agent, status, created_at, updated_at
		FROM threads
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	threads := []models.Thread{}
	for rows.Next() {
		var t models.Thread
		err := rows.Scan(
			&t.ID, &t.ParentThreadID, &t.CreatedByAgent, &t.AssignedAgent,
			&t.Status, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			continue
		}
		threads = append(threads, t)
	}

	return c.JSON(http.StatusOK, threads)
}

func (h *Handler) GetThreadTasks(c echo.Context) error {
	threadID := c.Param("id")
	if threadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "thread id is required"})
	}

	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT payload, type, agent_id, thread_id, to_agents, observers, created_at 
		FROM tasks 
		WHERE thread_id = $1
		ORDER BY created_at ASC
	`, threadID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	tasks := []models.MessageEnvelope{}
	for rows.Next() {
		var payload []byte
		var msgType, agentID string
		var tID *string
		var toAgents, observers []string
		var createdAt time.Time
		if err := rows.Scan(&payload, &msgType, &agentID, &tID, &toAgents, &observers, &createdAt); err != nil {
			continue
		}

		tasks = append(tasks, models.MessageEnvelope{
			Type:      msgType,
			ThreadID:  tID,
			From:      agentID,
			To:        toAgents,
			Observers: observers,
			Timestamp: createdAt.Unix(),
			Payload:   payload,
		})
	}

	return c.JSON(http.StatusOK, tasks)
}

func (h *Handler) DeleteThread(c echo.Context) error {
	threadID := c.Param("id")
	if threadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "thread id is required"})
	}

	ctx := c.Request().Context()
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start transaction"})
	}
	defer tx.Rollback(ctx)

	// 1. Delete associated tasks
	_, err = tx.Exec(ctx, "DELETE FROM tasks WHERE thread_id = $1", threadID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete tasks"})
	}

	// 2. Delete the thread
	_, err = tx.Exec(ctx, "DELETE FROM threads WHERE id = $1", threadID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete thread"})
	}

	if err := tx.Commit(ctx); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to commit transaction"})
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) ListS3Files(c echo.Context) error {
	prefix := c.QueryParam("prefix")
	if prefix == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "prefix is required"})
	}

	files, err := h.Storage.ListFiles(c.Request().Context(), prefix)
	if err != nil {
		c.Logger().Errorf("failed to list files: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
	}

	return c.JSON(http.StatusOK, map[string]interface{}{"files": files})
}

func (h *Handler) GetS3PresignedURL(c echo.Context) error {
	key := c.QueryParam("key")
	if key == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "key is required"})
	}

	url, err := h.Storage.GetPresignedURL(c.Request().Context(), key)
	if err != nil {
		c.Logger().Errorf("failed to get presigned url: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
	}

	return c.JSON(http.StatusOK, map[string]string{"url": url})
}

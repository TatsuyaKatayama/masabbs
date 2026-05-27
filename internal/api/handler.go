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
	api.GET("/agents", h.GetAgents)
	api.POST("/agents", h.CreateAgent)
	api.POST("/agents/:id/credentials", h.GenerateCredentials)
	api.GET("/tasks", h.GetTasks)

	// WebSocket for Admin UI

	e.GET("/ws", func(c echo.Context) error {
		hub.ServeWS(c.Response(), c.Request())
		return nil
	})
}

type CreateThreadRequest struct {
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

	// 1. Generate ULID
	threadID := ulid.Make().String()
	inputDir := h.Storage.GetThreadInputPath(threadID)

	// 2. Create S3 Folders (First, as it's hardest to roll back)
	if err = h.Storage.CreateThreadFolders(ctx, threadID); err != nil {
		c.Logger().Errorf("failed to create s3 folders: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
	}

	// 3. Create DB record (First, so Archiver can find it)
	_, err = h.DB.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status)
		VALUES ($1, $2, $3, 'open')
	`, threadID, req.ParentThreadID, req.CreatedByAgent)
	if err != nil {
		c.Logger().Errorf("failed to insert thread: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database record creation failed"})
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
		// Note: Thread is already in DB, but task message failed. 
		// In a production system, we might want to use a transaction or outbox pattern.
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "messaging error"})
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
		SELECT payload, type, agent_id, thread_id, created_at 
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
		var payload []byte
		var msgType, agentID string
		var threadID *string
		var createdAt time.Time
		if err := rows.Scan(&payload, &msgType, &agentID, &threadID, &createdAt); err != nil {
			continue
		}

		tasks = append(tasks, models.MessageEnvelope{
			Type:      msgType,
			ThreadID:  threadID,
			From:      agentID,
			Timestamp: createdAt.Unix(),
			Payload:   payload,
		})
	}

	return c.JSON(http.StatusOK, tasks)
}

type CreateAgentRequest struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Role   string  `json:"role"`
	TeamID *string `json:"team_id,omitempty"`
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
		INSERT INTO agents (id, name, role, status, team_id)
		VALUES ($1, $2, $3, 'offline', $4)
	`, req.ID, req.Name, req.Role, req.TeamID)
	if err != nil {
		c.Logger().Errorf("failed to create agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create agent"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"message": "agent created successfully"})
}

func (h *Handler) GetAgents(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, name, role, status, team_id, created_at, updated_at, tools, capabilities
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
			&a.ID, &a.Name, &a.Role, &a.Status, &a.TeamID,
			&a.CreatedAt, &a.UpdatedAt, &a.Tools, &a.Capabilities,
		)
		if err != nil {
			continue
		}
		agents = append(agents, a)
	}

	return c.JSON(http.StatusOK, agents)
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

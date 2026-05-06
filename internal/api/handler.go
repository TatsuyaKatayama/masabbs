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
	var req CreateThreadRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.Command == "" || req.CreatedByAgent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "command and created_by_agent are required"})
	}

	// 1. Generate ULID
	threadID := ulid.Make().String()
	inputDir := h.Storage.GetThreadInputPath(threadID)

	// 2. Create S3 Folders (First, as it's hardest to roll back)
	if err := h.Storage.CreateThreadFolders(ctx, threadID); err != nil {
		c.Logger().Errorf("failed to create s3 folders: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
	}

	// 3. Publish to NATS
	taskPayload := models.TaskPayload{
		Command:  req.Command,
		InputDir: inputDir,
		Deadline: req.Deadline,
	}
	payloadBytes, err := json.Marshal(taskPayload)
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
	envelopeBytes, err := json.Marshal(envelope)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal encoding error"})
	}

	subject := fmt.Sprintf("board.task.%s", threadID)
	if _, err = h.NATS.JS.Publish(ctx, subject, envelopeBytes); err != nil {
		c.Logger().Errorf("failed to publish to nats: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "messaging error"})
	}

	// 4. Create DB record (Last, as SSOT)
	_, err = h.DB.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status)
		VALUES ($1, $2, $3, 'open')
	`, threadID, req.ParentThreadID, req.CreatedByAgent)
	if err != nil {
		c.Logger().Errorf("failed to insert thread: %v", err)
		// At this point, S3 and NATS are done, but DB failed.
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database record creation failed"})
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

	var tasks []models.MessageEnvelope
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

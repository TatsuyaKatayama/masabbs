package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/mentions"
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
	Resolver     *mentions.Resolver
}

// RegisterRoutes sets up all API endpoints
func RegisterRoutes(e *echo.Echo, db *pgxpool.Pool, nc *nats.Client, sc storage.StorageProvider, hub *Hub, authProvider *auth.Provider) {
	h := &Handler{
		DB:           db,
		NATS:         nc,
		Storage:      sc,
		AuthProvider: authProvider,
		Resolver:     mentions.NewResolver(db),
	}
	api := e.Group("/api/v1")
	api.GET("/health", h.HealthCheck)
	api.POST("/threads", h.CreateThread)
	api.GET("/threads", h.GetThreads)
	api.GET("/threads/:id", h.GetThread)
	api.POST("/threads/:id/messages", h.PostMessage)
	api.GET("/threads/:id/tasks", h.GetThreadTasks)
	api.DELETE("/threads/:id", h.DeleteThread)
	api.POST("/threads/:id/reflection-requests", h.RequestReflection)
	api.POST("/reflections", h.SubmitReflection)
	api.GET("/threads/:id/kpi", h.GetThreadKPI)
	api.GET("/teams/:id/kpi", h.GetTeamKPI)
	api.GET("/agents", h.GetAgents)
	api.POST("/agents", h.CreateAgent)
	api.GET("/agents/:id", h.GetAgent)
	api.PATCH("/agents/:id", h.UpdateAgent)
	api.DELETE("/agents/:id", h.DeleteAgent)
	api.GET("/agents/:id/network", h.GetAgentNetwork)
	api.GET("/teams/:id/blueprint", h.GetTeamBlueprint)

	api.GET("/tasks", h.GetTasks)
	api.GET("/storage/files", h.ListS3Files)
	api.GET("/storage/presign", h.GetS3PresignedURL)

	api.GET("/teams", h.GetTeams)
	api.POST("/teams", h.CreateTeam)
	api.PATCH("/teams/:id", h.UpdateTeam)
	api.DELETE("/teams/:id", h.DeleteTeam)
	api.GET("/teams/:id/agents", h.GetTeamAgents)
	api.POST("/teams/:id/agents/:agent_id", h.AddTeamAgent)
	api.DELETE("/teams/:id/agents/:agent_id", h.RemoveTeamAgent)
	api.GET("/teams/:id/relations", h.GetTeamRelations)
	api.POST("/relations", h.CreateRelation)
	api.DELETE("/relations/:id", h.DeleteRelation)

	// Presets Configuration Endpoints
	api.GET("/configs", h.GetConfigs)
	api.POST("/configs", h.CreateConfig)
	api.DELETE("/configs/:id", h.DeleteConfig)
	api.POST("/configs/:id/load", h.LoadConfig)
	RegisterThreadSnapshotRoutes(e, db)
	api.GET("/configs/export", h.ExportConfig)
	api.POST("/configs/import", h.ImportConfig)
	api.GET("/snapshot/export", h.ExportSnapshot)
	api.POST("/snapshot/import", h.ImportSnapshot)

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
	TeamID         *string  `json:"team_id,omitempty"`
}

type CreateThreadResponse struct {
	ThreadID string `json:"thread_id"`
	InputDir string `json:"input_dir"`
}

type PostMessageRequest struct {
	FromAgent string                 `json:"from_agent"`
	Message   string                 `json:"message"`
	OutputDir string                 `json:"output_dir,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
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

	// Step 4: Permission Check
	var role string
	err = h.DB.QueryRow(ctx, "SELECT role FROM agents WHERE id = $1", req.CreatedByAgent).Scan(&role)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "AGENT_NOT_FOUND"})
	}

	normalizedRole := models.NormalizeRole(role)

	var teamID *string
	if req.TeamID != nil && *req.TeamID != "" {
		teamID = req.TeamID
	}

	if req.ParentThreadID == nil || *req.ParentThreadID == "" {
		// Top-level thread
		if !models.CanCreateTopLevelThread(normalizedRole) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "PERMISSION_DENIED: only TeamManager can create top-level threads"})
		}
		// If TeamID is not explicitly provided, fetch the creator agent's team_id
		if teamID == nil {
			var agentTeamID *string
			err = h.DB.QueryRow(ctx, "SELECT team_id FROM agents WHERE id = $1", req.CreatedByAgent).Scan(&agentTeamID)
			if err == nil && agentTeamID != nil && *agentTeamID != "" {
				teamID = agentTeamID
			}
		}
	} else {
		// Subthread
		if !models.CanCreateSubthread(normalizedRole) {
			return c.JSON(http.StatusForbidden, map[string]string{"error": "PERMISSION_DENIED: only TeamManager or Chef can create subthreads"})
		}

		// Inherit parent's team_id if not explicitly provided
		var parentTeamID *string
		err = h.DB.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = $1", *req.ParentThreadID).Scan(&parentTeamID)
		if err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "PARENT_THREAD_NOT_FOUND"})
		}
		if teamID == nil {
			teamID = parentTeamID
		}

		// Chef-specific validation: must be a member of the parent thread's team
		if normalizedRole == models.RoleChef {
			if teamID == nil {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "PERMISSION_DENIED: parent thread is not associated with any team"})
			}
			var belongs bool
			err = h.DB.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM team_agents WHERE team_id = $1 AND agent_id = $2
					UNION
					SELECT 1 FROM agents WHERE id = $2 AND team_id = $1
				)
			`, *teamID, req.CreatedByAgent).Scan(&belongs)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to check chef team membership"})
			}
			if !belongs {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "PERMISSION_DENIED: Chef is not a member of the parent thread's team"})
			}
		}
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
			INSERT INTO threads (id, parent_thread_id, created_by_agent, status, team_id)
			VALUES ($1, $2, $3, 'open', $4)
		`, threadID, req.ParentThreadID, req.CreatedByAgent, teamID)
		if err != nil {
			c.Logger().Errorf("failed to insert thread: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database record creation failed"})
		}
	}

	// Resolve mentions authoritative
	resolveRes := h.Resolver.Resolve(ctx, req.Command, threadID, req.CreatedByAgent)
	if resolveRes.ErrorCode != "" {
		if !isExisting {
			h.DB.Exec(ctx, "DELETE FROM threads WHERE id = $1", threadID)
		}
		return c.JSON(http.StatusBadRequest, map[string]string{"error": resolveRes.ErrorCode})
	}
	req.To = resolveRes.ToAgents

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

func (h *Handler) HealthCheck(c echo.Context) error {
	if err := h.DB.Ping(c.Request().Context()); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "db error"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Mission string `json:"mission,omitempty"`
}

func (h *Handler) CreateAgent(c echo.Context) error {
	var req CreateAgentRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.ID == "" || req.Name == "" || req.Role == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "id, name, and role are required"})
	}

	role := models.NormalizeRole(req.Role)

	_, err := h.DB.Exec(c.Request().Context(), `
		INSERT INTO agents (id, name, role, mission, status)
		VALUES ($1, $2, $3, $4, 'offline')
	`, req.ID, req.Name, role, req.Mission)
	if err != nil {
		c.Logger().Errorf("failed to create agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create agent"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"message": "agent created successfully"})
}

func (h *Handler) GetAgents(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, name, role, mission, status, team_id, ui_pos_x, ui_pos_y, created_at, updated_at, tools, capabilities
		FROM agents
		ORDER BY created_at DESC
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
			&a.UIPosX, &a.UIPosY,
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
	Name    *string  `json:"name,omitempty"`
	Role    *string  `json:"role,omitempty"`
	Mission *string  `json:"mission,omitempty"`
	TeamID  *string  `json:"team_id,omitempty"`
	UIPosX  *float64 `json:"ui_pos_x,omitempty"`
	UIPosY  *float64 `json:"ui_pos_y,omitempty"`
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
		role := models.NormalizeRole(*req.Role)
		args = append(args, role)
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
	if req.UIPosX != nil {
		query += fmt.Sprintf(", ui_pos_x = $%d", argIdx)
		args = append(args, *req.UIPosX)
		argIdx++
	}
	if req.UIPosY != nil {
		query += fmt.Sprintf(", ui_pos_y = $%d", argIdx)
		args = append(args, *req.UIPosY)
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

func (h *Handler) DeleteAgent(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agent id is required"})
	}

	ctx := c.Request().Context()

	// With CASCADE delete on agent_relations, threads, tasks, and logs,
	// we just need to delete the agent record.
	res, err := h.DB.Exec(ctx, "DELETE FROM agents WHERE id = $1", id)
	if err != nil {
		c.Logger().Errorf("failed to delete agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete agent"})
	}

	if res.RowsAffected() == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	return c.NoContent(http.StatusNoContent)
}

type UpdateTeamRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Mission     *string `json:"mission,omitempty"`
}

type CreateTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Mission     string `json:"mission,omitempty"`
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

func (h *Handler) CreateTeam(c echo.Context) error {
	var req CreateTeamRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}
	if req.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "team name is required"})
	}

	teamID := ulid.Make().String()
	var team models.Team
	err := h.DB.QueryRow(c.Request().Context(), `
		INSERT INTO teams (id, name, description, mission)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, description, mission, created_at, updated_at
	`, teamID, req.Name, req.Description, req.Mission).Scan(
		&team.ID, &team.Name, &team.Description, &team.Mission, &team.CreatedAt, &team.UpdatedAt,
	)
	if err != nil {
		c.Logger().Errorf("failed to create team: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to create team"})
	}

	return c.JSON(http.StatusCreated, team)
}

func (h *Handler) GetTeamAgents(c echo.Context) error {
	teamID := c.Param("id")
	if teamID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "team id is required"})
	}

	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT a.id, a.name, a.role, a.mission, a.status, a.team_id, a.ui_pos_x, a.ui_pos_y, a.created_at, a.updated_at, a.tools, a.capabilities
		FROM agents a
		INNER JOIN team_agents ta ON ta.agent_id = a.id
		WHERE ta.team_id = $1
		ORDER BY a.created_at DESC
	`, teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	agents := []models.Agent{}
	for rows.Next() {
		var agent models.Agent
		if err := rows.Scan(
			&agent.ID, &agent.Name, &agent.Role, &agent.Mission, &agent.Status, &agent.TeamID,
			&agent.UIPosX, &agent.UIPosY, &agent.CreatedAt, &agent.UpdatedAt, &agent.Tools, &agent.Capabilities,
		); err != nil {
			continue
		}
		agents = append(agents, agent)
	}

	return c.JSON(http.StatusOK, agents)
}

func (h *Handler) AddTeamAgent(c echo.Context) error {
	teamID := c.Param("id")
	agentID := c.Param("agent_id")
	if teamID == "" || agentID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "team id and agent id are required"})
	}

	_, err := h.DB.Exec(c.Request().Context(), `
		INSERT INTO team_agents (team_id, agent_id)
		VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, teamID, agentID)
	if err != nil {
		c.Logger().Errorf("failed to add team agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to add team agent"})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "agent added to team"})
}

func (h *Handler) RemoveTeamAgent(c echo.Context) error {
	teamID := c.Param("id")
	agentID := c.Param("agent_id")
	if teamID == "" || agentID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "team id and agent id are required"})
	}

	ctx := c.Request().Context()
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "cannot start transaction"})
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM agent_relations WHERE team_id = $1 AND (source_id = $2 OR target_id = $2)", teamID, agentID); err != nil {
		c.Logger().Errorf("failed to remove team agent relations: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to remove team agent relations"})
	}

	res, err := tx.Exec(ctx, "DELETE FROM team_agents WHERE team_id = $1 AND agent_id = $2", teamID, agentID)
	if err != nil {
		c.Logger().Errorf("failed to remove team agent: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to remove team agent"})
	}
	if res.RowsAffected() == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "team agent not found"})
	}

	if err := tx.Commit(ctx); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "transaction commit failed"})
	}

	return c.NoContent(http.StatusNoContent)
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

func (h *Handler) DeleteTeam(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "team id is required"})
	}

	res, err := h.DB.Exec(c.Request().Context(), "DELETE FROM teams WHERE id = $1", id)
	if err != nil {
		c.Logger().Errorf("failed to delete team: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete team"})
	}
	if res.RowsAffected() == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "team not found"})
	}

	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetThreads(c echo.Context) error {
	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at
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
			&t.Status, &t.TeamID, &t.CreatedAt, &t.UpdatedAt,
		)
		if err != nil {
			continue
		}
		threads = append(threads, t)
	}

	return c.JSON(http.StatusOK, threads)
}

func (h *Handler) GetThread(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "thread id is required"})
	}

	var t models.Thread
	err := h.DB.QueryRow(c.Request().Context(), `
		SELECT id, parent_thread_id, created_by_agent, assigned_agent, status, team_id, created_at, updated_at
		FROM threads
		WHERE id = $1
	`, id).Scan(
		&t.ID, &t.ParentThreadID, &t.CreatedByAgent, &t.AssignedAgent,
		&t.Status, &t.TeamID, &t.CreatedAt, &t.UpdatedAt,
	)

	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "thread not found"})
	}

	return c.JSON(http.StatusOK, t)
}

func (h *Handler) PostMessage(c echo.Context) error {
	ctx := c.Request().Context()
	threadID := c.Param("id")
	var req PostMessageRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.FromAgent == "" || req.Message == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "from_agent and message are required"})
	}

	// 1. Verify agent exists
	var exists bool
	err := h.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1)", req.FromAgent).Scan(&exists)
	if err != nil || !exists {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "AGENT_NOT_FOUND"})
	}

	// 2. Fetch thread and check status
	var status string
	err = h.DB.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "THREAD_NOT_FOUND"})
	}

	if status == "done" || status == "error" {
		return c.JSON(http.StatusConflict, map[string]string{"error": "THREAD_CLOSED"})
	}

	// 3. Resolve mentions authoritative
	resolveRes := h.Resolver.Resolve(ctx, req.Message, threadID, req.FromAgent)
	if resolveRes.ErrorCode != "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": resolveRes.ErrorCode})
	}

	// 4. Save to DB (tasks table)
	taskID := ulid.Make().String()
	payload := models.ResultPayload{
		OutputDir: req.OutputDir,
		Message:   req.Message,
		Error:     req.Error,
		ExitCode:  0, // Default for conversation messages
	}
	payloadBytes, _ := json.Marshal(payload)

	_, err = h.DB.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, payload)
		VALUES ($1, $2, $3, 'result', $4, $5)
	`, taskID, threadID, req.FromAgent, resolveRes.ToAgents, payloadBytes)
	if err != nil {
		c.Logger().Errorf("failed to insert task: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}

	// 5. Update thread updated_at
	h.DB.Exec(ctx, "UPDATE threads SET updated_at = CURRENT_TIMESTAMP WHERE id = $1", threadID)

	// 6. Publish to NATS
	envelope := models.MessageEnvelope{
		ID:        taskID,
		Type:      "result",
		ThreadID:  &threadID,
		From:      req.FromAgent,
		To:        resolveRes.ToAgents,
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	}
	envelopeBytes, _ := json.Marshal(envelope)
	subject := fmt.Sprintf("board.result.%s", threadID)
	if _, err := h.NATS.JS.Publish(ctx, subject, envelopeBytes); err != nil {
		c.Logger().Errorf("failed to publish to nats: %v", err)
		// We return 500 because DB is already updated but NATS failed.
		// In a real system, we'd want this to be atomic or use outbox.
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "messaging error"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"id": taskID})
}

func (h *Handler) GetThreadTasks(c echo.Context) error {
	threadID := c.Param("id")
	if threadID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "thread id is required"})
	}

	rows, err := h.DB.Query(c.Request().Context(), `
		SELECT id, payload, type, agent_id, thread_id, to_agents, observers, created_at
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
		var id string
		var payload []byte
		var msgType, agentID string
		var tID *string
		var toAgents, observers []string
		var createdAt time.Time
		if err := rows.Scan(&id, &payload, &msgType, &agentID, &tID, &toAgents, &observers, &createdAt); err != nil {
			continue
		}

		tasks = append(tasks, models.MessageEnvelope{
			ID:        id,
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

	// CASCADE is set on tasks, threads (parent_thread_id), and logs.
	// Deleting the thread will automatically delete all associated data.
	res, err := h.DB.Exec(ctx, "DELETE FROM threads WHERE id = $1", threadID)
	if err != nil {
		c.Logger().Errorf("failed to delete thread: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete thread"})
	}

	if res.RowsAffected() == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "thread not found"})
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

// GetAgentNetwork returns the adjacent agents and their relative relations (v1.2.0)
func (h *Handler) GetAgentNetwork(c echo.Context) error {
	ctx := c.Request().Context()
	agentID := c.Param("id")

	// TODO: Authorization check (JWT subject must match agentID)

	query := `
		SELECT 
			CASE 
				WHEN r.source_id = $1 THEN r.target_id 
				ELSE r.source_id 
			END as adjacent_id,
			r.relation_type,
			r.relation_category,
			r.source_id,
			a.mission,
			a.status,
			a.capabilities
		FROM agent_relations r
		JOIN agents a ON (
			CASE 
				WHEN r.source_id = $1 THEN r.target_id = a.id 
				ELSE r.source_id = a.id 
			END
		)
		WHERE r.source_id = $1 OR r.target_id = $1
	`

	rows, err := h.DB.Query(ctx, query, agentID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query network"})
	}
	defer rows.Close()

	var network []models.NetworkMember
	for rows.Next() {
		var member models.NetworkMember
		var relType, relCat, sourceID string
		if err := rows.Scan(
			&member.AgentID,
			&relType,
			&relCat,
			&sourceID,
			&member.Mission,
			&member.Status,
			&member.Capabilities,
		); err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to scan network member"})
		}

		// Relative role transformation
		transformedType := relType
		if relCat == "vertical" {
			if sourceID == agentID {
				// I am the boss (Source)
				transformedType = "subordinate"
			} else {
				// I am the subordinate (Target)
				transformedType = "boss"
			}
		} else {
			// Horizontal/Coworker - symmetric
			transformedType = "coworker"
		}

		member.Relation = models.RelationInfo{
			Category: relCat,
			Type:     transformedType,
		}
		network = append(network, member)
	}

	return c.JSON(http.StatusOK, network)
}

// GetTeamBlueprint returns the Mermaid diagram and member profiles (v1.2.0)
func (h *Handler) GetTeamBlueprint(c echo.Context) error {
	ctx := c.Request().Context()
	teamID := c.Param("id")

	// 1. Fetch all members of the team
	rows, err := h.DB.Query(ctx, `
		SELECT a.id, a.name, a.role, a.mission, a.status, a.team_id, a.ui_pos_x, a.ui_pos_y, a.created_at, a.updated_at, a.tools, a.capabilities
		FROM agents a
		INNER JOIN team_agents ta ON ta.agent_id = a.id
		WHERE ta.team_id = $1
	`, teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch team members"})
	}
	defer rows.Close()

	var blueprint models.TeamBlueprint
	blueprint.TeamID = teamID

	mermaid := "graph TD\n"
	for rows.Next() {
		var a models.Agent
		if err := rows.Scan(&a.ID, &a.Name, &a.Role, &a.Mission, &a.Status, &a.TeamID, &a.UIPosX, &a.UIPosY, &a.CreatedAt, &a.UpdatedAt, &a.Tools, &a.Capabilities); err != nil {
			continue
		}
		blueprint.Members = append(blueprint.Members, a)
		// Define node to handle isolated agents: AgentID["Name (Role)"]
		mermaid += fmt.Sprintf("  %s[\"%s (%s)\"]\n", a.ID, a.Name, a.Role)
	}

	// 2. Fetch all relations in the team for edges
	relRows, err := h.DB.Query(ctx, "SELECT source_id, target_id, relation_type FROM agent_relations WHERE team_id = $1", teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch relations"})
	}
	defer relRows.Close()

	for relRows.Next() {
		var src, tgt, rType string
		if err := relRows.Scan(&src, &tgt, &rType); err != nil {
			continue
		}

		if rType == "boss" {
			// A commands B
			mermaid += fmt.Sprintf("  %s -- \"instruct\" --> %s\n", src, tgt)
		} else if rType == "coworker" {
			// A and B cooperate
			mermaid += fmt.Sprintf("  %s -- \"cooperates with\" <--> %s\n", src, tgt)
		} else {
			// Fallback for any other types
			mermaid += fmt.Sprintf("  %s -- \"%s\" --> %s\n", src, rType, tgt)
		}
	}
	blueprint.StructureMermaid = mermaid

	return c.JSON(http.StatusOK, blueprint)
}

type CreateRelationRequest struct {
	TeamID       string `json:"team_id"`
	SourceID     string `json:"source_id"`
	TargetID     string `json:"target_id"`
	SourceHandle string `json:"source_handle"`
	TargetHandle string `json:"target_handle"`
	RelationType string `json:"relation_type"`
}

func (h *Handler) CreateRelation(c echo.Context) error {
	ctx := c.Request().Context()
	var req CreateRelationRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	// Validate preset relation types and derive category
	var category string
	switch req.RelationType {
	case "boss":
		category = "vertical"
	case "coworker":
		category = "horizontal"
	default:
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid relation_type. Use 'boss' or 'coworker'"})
	}

	// Application-level validation: Source and Target must belong to the same team
	var sameTeam bool
	err := h.DB.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM team_agents WHERE team_id = $1 AND agent_id = $2)
			AND EXISTS(SELECT 1 FROM team_agents WHERE team_id = $1 AND agent_id = $3)
	`, req.TeamID, req.SourceID, req.TargetID).Scan(&sameTeam)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to validate team membership"})
	}
	if !sameTeam {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "agents must belong to the same team"})
	}

	id := ulid.Make().String()
	_, err = h.DB.Exec(ctx, `
		INSERT INTO agent_relations (id, team_id, source_id, target_id, source_handle, target_handle, relation_type, relation_category)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (source_id, target_id, relation_type) 
		DO UPDATE SET 
			source_handle = EXCLUDED.source_handle,
			target_handle = EXCLUDED.target_handle,
			team_id = EXCLUDED.team_id,
			relation_category = EXCLUDED.relation_category
	`, id, req.TeamID, req.SourceID, req.TargetID, req.SourceHandle, req.TargetHandle, req.RelationType, category)

	if err != nil {
		c.Logger().Errorf("failed to create relation: %v", err)
		return c.JSON(http.StatusConflict, map[string]string{"error": "relation already exists or database error"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"id": id})
}

func (h *Handler) DeleteRelation(c echo.Context) error {
	id := c.Param("id")
	_, err := h.DB.Exec(c.Request().Context(), "DELETE FROM agent_relations WHERE id = $1", id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to delete relation"})
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) GetTeamRelations(c echo.Context) error {
	ctx := c.Request().Context()
	teamID := c.Param("id")

	rows, err := h.DB.Query(ctx, "SELECT id, team_id, source_id, target_id, source_handle, target_handle, relation_type, relation_category FROM agent_relations WHERE team_id = $1", teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch relations"})
	}
	defer rows.Close()

	var relations []models.AgentRelation
	for rows.Next() {
		var r models.AgentRelation
		if err := rows.Scan(&r.ID, &r.TeamID, &r.SourceID, &r.TargetID, &r.SourceHandle, &r.TargetHandle, &r.RelationType, &r.RelationCategory); err != nil {
			continue
		}
		relations = append(relations, r)
	}

	return c.JSON(http.StatusOK, relations)
}

type RequestReflectionRequest struct {
	RequestedByAgent string     `json:"requested_by_agent"`
	DueAt            *time.Time `json:"due_at,omitempty"`
}

type RequestReflectionResponse struct {
	RequestID          string `json:"request_id"`
	ReflectionThreadID string `json:"reflection_thread_id"`
}

type SubmitReflectionRequest struct {
	RequestID     string `json:"request_id"`
	FromAgentID   string `json:"from_agent_id"`
	TargetAgentID string `json:"target_agent_id"`
	Dimension     string `json:"dimension"`
	Score         int    `json:"score"`
	Reason        string `json:"reason"`
	Suggestion    string `json:"suggestion,omitempty"`
}

func (h *Handler) RequestReflection(c echo.Context) error {
	ctx := c.Request().Context()
	parentThreadID := c.Param("id")

	var req RequestReflectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.RequestedByAgent == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "requested_by_agent is required"})
	}

	// 1. Fetch parent thread details to get team_id
	var teamID *string
	err := h.DB.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = $1", parentThreadID).Scan(&teamID)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "THREAD_NOT_FOUND"})
	}
	if teamID == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "parent thread must be associated with a team to request reflection"})
	}

	// 2. Fetch all unique agents in the same team
	rows, err := h.DB.Query(ctx, `
		SELECT agent_id FROM team_agents WHERE team_id = $1
		UNION
		SELECT id AS agent_id FROM agents WHERE team_id = $1
	`, *teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch team agents"})
	}
	defer rows.Close()

	var teamAgents []string
	for rows.Next() {
		var aid string
		if err := rows.Scan(&aid); err == nil {
			teamAgents = append(teamAgents, aid)
		}
	}

	if len(teamAgents) == 0 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "NO_TEAM_MEMBERS"})
	}

	// 3. Create automatic reflection subthread (using the subthread workflow)
	reflectionThreadID := ulid.Make().String()
	reflectionInputDir := h.Storage.GetThreadInputPath(reflectionThreadID)

	// Create S3 Folders
	if err = h.Storage.CreateThreadFolders(ctx, reflectionThreadID); err != nil {
		c.Logger().Errorf("failed to create s3 folders: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "storage error"})
	}

	// Create DB record for the subthread
	_, err = h.DB.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status, team_id)
		VALUES ($1, $2, $3, 'open', $4)
	`, reflectionThreadID, parentThreadID, req.RequestedByAgent, teamID)
	if err != nil {
		c.Logger().Errorf("failed to insert subthread: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}

	// Generate due_at (default to 24 hours from now if empty)
	dueAt := time.Now().Add(24 * time.Hour)
	if req.DueAt != nil {
		dueAt = *req.DueAt
	}

	// 4. Create record in thread_reflection_requests
	requestID := ulid.Make().String()
	_, err = h.DB.Exec(ctx, `
		INSERT INTO thread_reflection_requests (id, thread_id, reflection_thread_id, requested_by_agent_id, status, due_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
	`, requestID, parentThreadID, reflectionThreadID, req.RequestedByAgent, dueAt)
	if err != nil {
		c.Logger().Errorf("failed to insert reflection request: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}

	// 5. Build reflection task command/message with all team agent mentions (excluding requesting agent)
	mentionText := ""
	for _, aid := range teamAgents {
		if aid != req.RequestedByAgent {
			mentionText += " @" + aid
		}
	}
	reflectionCommand := fmt.Sprintf("[Reflection] Please submit your reflection for thread %s.%s", parentThreadID, mentionText)

	// Publish Reflection task message to NATS
	taskPayload := models.TaskPayload{
		Command:  reflectionCommand,
		InputDir: reflectionInputDir,
		Deadline: dueAt.Format(time.RFC3339),
	}
	payloadBytes, _ := json.Marshal(taskPayload)

	envelope := models.MessageEnvelope{
		Type:      "task",
		ThreadID:  &reflectionThreadID,
		From:      req.RequestedByAgent,
		To:        teamAgents,
		Timestamp: time.Now().Unix(),
		Payload:   payloadBytes,
	}
	envelopeBytes, _ := json.Marshal(envelope)

	subject := fmt.Sprintf("board.task.%s", reflectionThreadID)
	if _, err = h.NATS.JS.Publish(ctx, subject, envelopeBytes); err != nil {
		c.Logger().Errorf("failed to publish reflection task: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "messaging error"})
	}

	return c.JSON(http.StatusCreated, RequestReflectionResponse{
		RequestID:          requestID,
		ReflectionThreadID: reflectionThreadID,
	})
}

func (h *Handler) SubmitReflection(c echo.Context) error {
	ctx := c.Request().Context()

	var req SubmitReflectionRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if req.RequestID == "" || req.FromAgentID == "" || req.TargetAgentID == "" || req.Dimension == "" || req.Reason == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "request_id, from_agent_id, target_agent_id, dimension, and reason are required"})
	}

	if req.Score < -1 || req.Score > 1 {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "score must be -1, 0, or 1"})
	}

	// 1. Fetch reflection request details
	var threadID string
	var dueAt time.Time
	err := h.DB.QueryRow(ctx, "SELECT thread_id, due_at FROM thread_reflection_requests WHERE id = $1", req.RequestID).Scan(&threadID, &dueAt)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "REFLECTION_REQUEST_NOT_FOUND"})
	}

	// 2. Check if due_at has expired
	if time.Now().After(dueAt) {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "REFLECTION_REQUEST_EXPIRED"})
	}

	// 3. Fetch team_id associated with the thread
	var teamID string
	err = h.DB.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = $1", threadID).Scan(&teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch thread team"})
	}

	// 4. Validate that both from_agent_id and target_agent_id belong to the same team_id
	var exists bool
	err = h.DB.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM team_agents ta1
			JOIN team_agents ta2 ON ta1.team_id = ta2.team_id
			WHERE ta1.team_id = $1 AND ta1.agent_id = $2 AND ta2.agent_id = $3
		) OR EXISTS (
			SELECT 1 FROM agents a1
			JOIN agents a2 ON a1.team_id = a2.team_id
			WHERE a1.team_id = $1 AND a1.id = $2 AND a2.id = $3
		)
	`, teamID, req.FromAgentID, req.TargetAgentID).Scan(&exists)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to validate agent relationships"})
	}

	if !exists {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "INVALID_TARGET_AGENT"})
	}

	// 5. Save/Upsert reflection into database (ON CONFLICT DO UPDATE)
	reflectionID := ulid.Make().String()
	var suggestionVal *string
	if req.Suggestion != "" {
		suggestionVal = &req.Suggestion
	}

	_, err = h.DB.Exec(ctx, `
		INSERT INTO thread_reflections (id, request_id, thread_id, team_id, from_agent_id, target_agent_id, dimension, score, reason, suggestion)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (request_id, from_agent_id, target_agent_id, dimension)
		DO UPDATE SET
			score = EXCLUDED.score,
			reason = EXCLUDED.reason,
			suggestion = EXCLUDED.suggestion,
			created_at = CURRENT_TIMESTAMP
	`, reflectionID, req.RequestID, threadID, teamID, req.FromAgentID, req.TargetAgentID, req.Dimension, req.Score, req.Reason, suggestionVal)

	if err != nil {
		c.Logger().Errorf("failed to save reflection: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save reflection"})
	}

	return c.JSON(http.StatusCreated, map[string]string{"id": reflectionID})
}

type KPIMessageStats struct {
	TotalMessages  int            `json:"total_messages"`
	SentCounts     map[string]int `json:"sent_counts"`
	ReceivedCounts map[string]int `json:"received_counts"`
}

type KPIReplyMetrics struct {
	AverageReplyDelaySeconds float64 `json:"average_reply_delay_seconds"`
	UnrepliedCount           int     `json:"unreplied_count"`
	UnrepliedRate            float64 `json:"unreplied_rate"`
}

type KPIReflectionStats struct {
	AverageScore float64            `json:"average_score"`
	ByDimension  map[string]float64 `json:"by_dimension"`
}

type KPINetworkNode struct {
	ID    string `json:"id"`
	Count int    `json:"count"`
}

type KPINetworkLink struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Value  int    `json:"value"`
}

type KPINetworkData struct {
	Nodes []KPINetworkNode `json:"nodes"`
	Links []KPINetworkLink `json:"links"`
}

type ThreadKPIResponse struct {
	ThreadID        string             `json:"thread_id"`
	SubthreadCount  int                `json:"subthread_count"`
	MaxDepth        int                `json:"max_depth"`
	MessageStats    KPIMessageStats    `json:"message_stats"`
	ReplyMetrics    KPIReplyMetrics    `json:"reply_metrics"`
	ReflectionStats KPIReflectionStats `json:"reflection_stats"`
	NetworkData     KPINetworkData     `json:"network_data"`
}

type TeamKPIResponse struct {
	TeamID            string             `json:"team_id"`
	TotalThreads      int                `json:"total_threads"`
	TotalSubthreads   int                `json:"total_subthreads"`
	MaxSubthreadDepth int                `json:"max_subthread_depth"`
	MessageStats      KPIMessageStats    `json:"message_stats"`
	ReplyMetrics      KPIReplyMetrics    `json:"reply_metrics"`
	ReflectionStats   KPIReflectionStats `json:"reflection_stats"`
	NetworkData       KPINetworkData     `json:"network_data"`
}

func (h *Handler) calculateKPIForThreads(ctx context.Context, threadIDs []string) (KPIMessageStats, KPIReplyMetrics, KPIReflectionStats, KPINetworkData, error) {
	msgStats := KPIMessageStats{
		SentCounts:     make(map[string]int),
		ReceivedCounts: make(map[string]int),
	}
	replyMetrics := KPIReplyMetrics{}
	reflStats := KPIReflectionStats{
		ByDimension: make(map[string]float64),
	}
	networkData := KPINetworkData{}

	if len(threadIDs) == 0 {
		return msgStats, replyMetrics, reflStats, networkData, nil
	}

	// 1. Fetch tasks
	rows, err := h.DB.Query(ctx, `
		SELECT id, thread_id, agent_id, type, to_agents, observers, created_at 
		FROM tasks 
		WHERE thread_id = ANY($1) 
		ORDER BY created_at ASC
	`, threadIDs)
	if err != nil {
		return msgStats, replyMetrics, reflStats, networkData, err
	}
	defer rows.Close()

	type taskRow struct {
		ID        string
		ThreadID  string
		AgentID   string
		Type      string
		ToAgents  []string
		Observers []string
		CreatedAt time.Time
	}

	var tasks []taskRow
	for rows.Next() {
		var tr taskRow
		if err := rows.Scan(&tr.ID, &tr.ThreadID, &tr.AgentID, &tr.Type, &tr.ToAgents, &tr.Observers, &tr.CreatedAt); err == nil {
			tasks = append(tasks, tr)
		}
	}

	msgStats.TotalMessages = len(tasks)

	// Interaction link map (Source -> Target -> count)
	interactions := make(map[string]map[string]int)

	type pendingKey struct {
		ThreadID string
		AgentID  string
	}
	pendingReplies := make(map[pendingKey]time.Time)
	var totalDelay time.Duration
	var replyCount int
	var totalRequests int

	for _, t := range tasks {
		// Update sent count
		msgStats.SentCounts[t.AgentID]++

		// Initialize inner interaction map if not exists
		if _, ok := interactions[t.AgentID]; !ok {
			interactions[t.AgentID] = make(map[string]int)
		}

		// Update received count and links
		for _, to := range t.ToAgents {
			if to != "" {
				msgStats.ReceivedCounts[to]++
				interactions[t.AgentID][to]++
			}
		}

		// Update reply latencies and unreplied requests
		pkey := pendingKey{ThreadID: t.ThreadID, AgentID: t.AgentID}
		if sentTime, ok := pendingReplies[pkey]; ok {
			totalDelay += t.CreatedAt.Sub(sentTime)
			replyCount++
			delete(pendingReplies, pkey)
		}

		if len(t.ToAgents) > 0 {
			for _, to := range t.ToAgents {
				if to != "" && to != t.AgentID {
					p := pendingKey{ThreadID: t.ThreadID, AgentID: to}
					pendingReplies[p] = t.CreatedAt
					totalRequests++
				}
			}
		}
	}

	if replyCount > 0 {
		replyMetrics.AverageReplyDelaySeconds = totalDelay.Seconds() / float64(replyCount)
	}
	replyMetrics.UnrepliedCount = len(pendingReplies)
	if totalRequests > 0 {
		replyMetrics.UnrepliedRate = float64(replyMetrics.UnrepliedCount) / float64(totalRequests)
	}

	// 2. Fetch Reflections
	refRows, err := h.DB.Query(ctx, `
		SELECT dimension, score 
		FROM thread_reflections 
		WHERE thread_id = ANY($1)
	`, threadIDs)
	if err == nil {
		defer refRows.Close()
		var totalScore int
		var refCount int
		dimScores := make(map[string]float64)
		dimCounts := make(map[string]int)

		for refRows.Next() {
			var dim string
			var score int
			if err := refRows.Scan(&dim, &score); err == nil {
				totalScore += score
				refCount++
				dimScores[dim] += float64(score)
				dimCounts[dim]++
			}
		}

		if refCount > 0 {
			reflStats.AverageScore = float64(totalScore) / float64(refCount)
			for dim, sum := range dimScores {
				reflStats.ByDimension[dim] = sum / float64(dimCounts[dim])
			}
		}
	}

	// 3. Build Nodes and Links for D3.js Network Data
	nodeSet := make(map[string]bool)
	for src, inner := range interactions {
		nodeSet[src] = true
		for tgt := range inner {
			nodeSet[tgt] = true
		}
	}

	for agentID := range nodeSet {
		networkData.Nodes = append(networkData.Nodes, KPINetworkNode{
			ID:    agentID,
			Count: msgStats.SentCounts[agentID],
		})
	}

	for src, inner := range interactions {
		for tgt, val := range inner {
			networkData.Links = append(networkData.Links, KPINetworkLink{
				Source: src,
				Target: tgt,
				Value:  val,
			})
		}
	}

	return msgStats, replyMetrics, reflStats, networkData, nil
}

func (h *Handler) GetThreadKPI(c echo.Context) error {
	ctx := c.Request().Context()
	threadID := c.Param("id")

	// 1. Fetch thread details to make sure it exists
	var exists bool
	err := h.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM threads WHERE id = $1)", threadID).Scan(&exists)
	if err != nil || !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "THREAD_NOT_FOUND"})
	}

	// 2. Query all subthreads recursively using PostgreSQL WITH RECURSIVE
	rows, err := h.DB.Query(ctx, `
		WITH RECURSIVE subthreads AS (
			SELECT id, parent_thread_id, 0 AS depth FROM threads WHERE id = $1
			UNION ALL
			SELECT t.id, t.parent_thread_id, s.depth + 1 FROM threads t
			INNER JOIN subthreads s ON t.parent_thread_id = s.id
		)
		SELECT id, depth FROM subthreads
	`, threadID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query subthreads"})
	}
	defer rows.Close()

	var threadIDs []string
	subthreadCount := -1 // exclude self
	maxDepth := 0
	for rows.Next() {
		var id string
		var depth int
		if err := rows.Scan(&id, &depth); err == nil {
			threadIDs = append(threadIDs, id)
			subthreadCount++
			if depth > maxDepth {
				maxDepth = depth
			}
		}
	}

	if subthreadCount < 0 {
		subthreadCount = 0
	}

	msgStats, replyMetrics, reflStats, networkData, err := h.calculateKPIForThreads(ctx, threadIDs)
	if err != nil {
		c.Logger().Errorf("failed to calculate KPI: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to calculate KPI"})
	}

	return c.JSON(http.StatusOK, ThreadKPIResponse{
		ThreadID:        threadID,
		SubthreadCount:  subthreadCount,
		MaxDepth:        maxDepth,
		MessageStats:    msgStats,
		ReplyMetrics:    replyMetrics,
		ReflectionStats: reflStats,
		NetworkData:     networkData,
	})
}

func (h *Handler) GetTeamKPI(c echo.Context) error {
	ctx := c.Request().Context()
	teamID := c.Param("id")

	// 1. Verify team exists
	var exists bool
	err := h.DB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM teams WHERE id = $1)", teamID).Scan(&exists)
	if err != nil || !exists {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "TEAM_NOT_FOUND"})
	}

	// 2. Fetch all root threads belonging to this team
	rows, err := h.DB.Query(ctx, "SELECT id FROM threads WHERE team_id = $1 AND (parent_thread_id IS NULL OR parent_thread_id = '')", teamID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query team threads"})
	}
	defer rows.Close()

	var rootThreadIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			rootThreadIDs = append(rootThreadIDs, id)
		}
	}

	totalThreads := len(rootThreadIDs)
	totalSubthreads := 0
	maxSubthreadDepth := 0

	var allThreadIDs []string

	// For each root thread, recursively find its child threads
	for _, rid := range rootThreadIDs {
		subRows, err := h.DB.Query(ctx, `
			WITH RECURSIVE subthreads AS (
				SELECT id, parent_thread_id, 0 AS depth FROM threads WHERE id = $1
				UNION ALL
				SELECT t.id, t.parent_thread_id, s.depth + 1 FROM threads t
				INNER JOIN subthreads s ON t.parent_thread_id = s.id
			)
			SELECT id, depth FROM subthreads
		`, rid)
		if err != nil {
			continue
		}
		defer subRows.Close()

		for subRows.Next() {
			var id string
			var depth int
			if err := subRows.Scan(&id, &depth); err == nil {
				allThreadIDs = append(allThreadIDs, id)
				if depth > 0 {
					totalSubthreads++
				}
				if depth > maxSubthreadDepth {
					maxSubthreadDepth = depth
				}
			}
		}
	}

	msgStats, replyMetrics, reflStats, networkData, err := h.calculateKPIForThreads(ctx, allThreadIDs)
	if err != nil {
		c.Logger().Errorf("failed to calculate team KPI: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to calculate KPI"})
	}

	return c.JSON(http.StatusOK, TeamKPIResponse{
		TeamID:            teamID,
		TotalThreads:      totalThreads,
		TotalSubthreads:   totalSubthreads,
		MaxSubthreadDepth: maxSubthreadDepth,
		MessageStats:      msgStats,
		ReplyMetrics:      replyMetrics,
		ReflectionStats:   reflStats,
		NetworkData:       networkData,
	})
}

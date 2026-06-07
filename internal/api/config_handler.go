package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/labstack/echo/v4"
	"github.com/oklog/ulid/v2"
)

type CreateConfigPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// GetConfigs lists all saved configurations
func (h *Handler) GetConfigs(c echo.Context) error {
	ctx := c.Request().Context()
	rows, err := h.DB.Query(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM configs
		ORDER BY created_at DESC
	`)
	if err != nil {
		c.Logger().Errorf("failed to get configs: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}
	defer rows.Close()

	configs := []models.Config{}
	for rows.Next() {
		var cfg models.Config
		err := rows.Scan(&cfg.ID, &cfg.Name, &cfg.Description, &cfg.CreatedAt, &cfg.UpdatedAt)
		if err != nil {
			continue
		}
		configs = append(configs, cfg)
	}

	return c.JSON(http.StatusOK, configs)
}

// CreateConfig saves the current state of teams, agents, and relations as a new configuration
func (h *Handler) CreateConfig(c echo.Context) error {
	ctx := c.Request().Context()
	var payload CreateConfigPayload
	if err := c.Bind(&payload); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format"})
	}

	if payload.Name == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "configuration name is required"})
	}

	snapshot, err := h.captureCurrentSnapshot(ctx)
	if err != nil {
		c.Logger().Errorf("failed to capture snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to capture snapshot"})
	}

	snapshotBytes, err := json.Marshal(snapshot)
	if err != nil {
		c.Logger().Errorf("failed to marshal snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "encoding error"})
	}

	configID := ulid.Make().String()
	var created models.Config
	err = h.DB.QueryRow(ctx, `
		INSERT INTO configs (id, name, description, data)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, description, created_at, updated_at
	`, configID, payload.Name, payload.Description, snapshotBytes).Scan(
		&created.ID, &created.Name, &created.Description, &created.CreatedAt, &created.UpdatedAt,
	)

	if err != nil {
		c.Logger().Errorf("failed to save config: %v", err)
		return c.JSON(http.StatusConflict, map[string]string{"error": "configuration name must be unique or database error"})
	}

	return c.JSON(http.StatusCreated, created)
}

// DeleteConfig deletes a configuration snapshot
func (h *Handler) DeleteConfig(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "config id is required"})
	}

	res, err := h.DB.Exec(ctx, "DELETE FROM configs WHERE id = $1", id)
	if err != nil {
		c.Logger().Errorf("failed to delete config: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "database error"})
	}

	if res.RowsAffected() == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "configuration not found"})
	}

	return c.NoContent(http.StatusNoContent)
}

// LoadConfig loads and applies a saved configuration snapshot
func (h *Handler) LoadConfig(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "config id is required"})
	}

	var dataBytes []byte
	err := h.DB.QueryRow(ctx, "SELECT data FROM configs WHERE id = $1", id).Scan(&dataBytes)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "configuration not found"})
	}

	var snapshot models.ConfigurationSnapshot
	if err := json.Unmarshal(dataBytes, &snapshot); err != nil {
		c.Logger().Errorf("failed to unmarshal loaded snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "malformed configuration data"})
	}

	if err := h.applySnapshot(ctx, &snapshot); err != nil {
		c.Logger().Errorf("failed to apply snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to apply configuration: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "configuration loaded successfully"})
}

// ExportConfig exports the current configurations as a downloadable JSON file
func (h *Handler) ExportConfig(c echo.Context) error {
	ctx := c.Request().Context()
	snapshot, err := h.captureCurrentSnapshot(ctx)
	if err != nil {
		c.Logger().Errorf("failed to capture snapshot for export: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to capture snapshot"})
	}

	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf("attachment; filename=masabbs-preset-%d.json", time.Now().Unix()))
	return c.JSON(http.StatusOK, snapshot)
}

// ImportConfig imports a configuration JSON payload and applies it
func (h *Handler) ImportConfig(c echo.Context) error {
	ctx := c.Request().Context()
	file, err := c.FormFile("file")
	if err != nil {
		// Fallback to JSON body if not multipart
		var snapshot models.ConfigurationSnapshot
		if err := c.Bind(&snapshot); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request format (missing file or invalid body)"})
		}
		if err := h.applySnapshot(ctx, &snapshot); err != nil {
			c.Logger().Errorf("failed to apply imported snapshot: %v", err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to apply configuration: %v", err)})
		}
		return c.JSON(http.StatusOK, map[string]string{"message": "configuration imported successfully"})
	}

	src, err := file.Open()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to open uploaded file"})
	}
	defer src.Close()

	var snapshot models.ConfigurationSnapshot
	if err := json.NewDecoder(src).Decode(&snapshot); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid JSON file format"})
	}

	if err := h.applySnapshot(ctx, &snapshot); err != nil {
		c.Logger().Errorf("failed to apply imported snapshot: %v", err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to apply configuration: %v", err)})
	}

	return c.JSON(http.StatusOK, map[string]string{"message": "configuration imported successfully"})
}

// Helper: captureCurrentSnapshot reads teams, agents, and relations from the database
func (h *Handler) captureCurrentSnapshot(ctx context.Context) (*models.ConfigurationSnapshot, error) {
	var snapshot models.ConfigurationSnapshot

	// 1. Capture Teams
	tRows, err := h.DB.Query(ctx, "SELECT id, name, description, mission, created_at, updated_at FROM teams")
	if err != nil {
		return nil, fmt.Errorf("failed to query teams: %w", err)
	}
	defer tRows.Close()

	for tRows.Next() {
		var t models.Team
		if err := tRows.Scan(&t.ID, &t.Name, &t.Description, &t.Mission, &t.CreatedAt, &t.UpdatedAt); err == nil {
			snapshot.Teams = append(snapshot.Teams, t)
		}
	}

	// 2. Capture Agents
	aRows, err := h.DB.Query(ctx, "SELECT id, name, role, mission, tools, capabilities, status, team_id, ui_pos_x, ui_pos_y, created_at, updated_at FROM agents")
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer aRows.Close()

	for aRows.Next() {
		var a models.Agent
		if err := aRows.Scan(&a.ID, &a.Name, &a.Role, &a.Mission, &a.Tools, &a.Capabilities, &a.Status, &a.TeamID, &a.UIPosX, &a.UIPosY, &a.CreatedAt, &a.UpdatedAt); err == nil {
			snapshot.Agents = append(snapshot.Agents, a)
		}
	}

	// 3. Capture Relations
	rRows, err := h.DB.Query(ctx, "SELECT id, team_id, source_id, target_id, source_handle, target_handle, relation_type, relation_category FROM agent_relations")
	if err != nil {
		return nil, fmt.Errorf("failed to query relations: %w", err)
	}
	defer rRows.Close()

	for rRows.Next() {
		var r models.AgentRelation
		if err := rRows.Scan(&r.ID, &r.TeamID, &r.SourceID, &r.TargetID, &r.SourceHandle, &r.TargetHandle, &r.RelationType, &r.RelationCategory); err == nil {
			snapshot.Relations = append(snapshot.Relations, r)
		}
	}

	return &snapshot, nil
}

// Helper: applySnapshot applies a snapshot configuration transactionally
func (h *Handler) applySnapshot(ctx context.Context, snap *models.ConfigurationSnapshot) error {
	tx, err := h.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Insert/Update Teams from snapshot
	for _, t := range snap.Teams {
		_, err = tx.Exec(ctx, `
			INSERT INTO teams (id, name, description, mission, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				mission = EXCLUDED.mission,
				updated_at = EXCLUDED.updated_at
		`, t.ID, t.Name, t.Description, t.Mission, t.CreatedAt, t.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to upsert team %s: %w", t.ID, err)
		}
	}

	// 2. Insert/Update Agents from snapshot
	snapAgentIDs := make(map[string]bool)
	for _, a := range snap.Agents {
		snapAgentIDs[a.ID] = true
		_, err = tx.Exec(ctx, `
			INSERT INTO agents (id, name, role, mission, tools, capabilities, status, team_id, ui_pos_x, ui_pos_y, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				role = EXCLUDED.role,
				mission = EXCLUDED.mission,
				tools = EXCLUDED.tools,
				capabilities = EXCLUDED.capabilities,
				status = EXCLUDED.status,
				team_id = EXCLUDED.team_id,
				ui_pos_x = EXCLUDED.ui_pos_x,
				ui_pos_y = EXCLUDED.ui_pos_y,
				updated_at = EXCLUDED.updated_at
		`, a.ID, a.Name, a.Role, a.Mission, a.Tools, a.Capabilities, a.Status, a.TeamID, a.UIPosX, a.UIPosY, a.CreatedAt, a.UpdatedAt)
		if err != nil {
			return fmt.Errorf("failed to upsert agent %s: %w", a.ID, err)
		}
	}

	// 3. Deactivate agents that are NOT in the loaded snapshot
	// Query current agent IDs in the DB
	rows, err := tx.Query(ctx, "SELECT id FROM agents")
	if err != nil {
		return err
	}
	var dbAgentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			dbAgentIDs = append(dbAgentIDs, id)
		}
	}
	rows.Close()

	for _, id := range dbAgentIDs {
		if !snapAgentIDs[id] {
			_, err = tx.Exec(ctx, "UPDATE agents SET team_id = NULL WHERE id = $1", id)
			if err != nil {
				return fmt.Errorf("failed to deactivate agent %s: %w", id, err)
			}
		}
	}

	// 4. Update Agent Relations from snapshot
	// Clear the old relations for teams loaded in this snapshot
	for _, t := range snap.Teams {
		_, err = tx.Exec(ctx, "DELETE FROM agent_relations WHERE team_id = $1", t.ID)
		if err != nil {
			return fmt.Errorf("failed to clear agent relations for team %s: %w", t.ID, err)
		}
	}

	// Insert relations
	for _, r := range snap.Relations {
		_, err = tx.Exec(ctx, `
			INSERT INTO agent_relations (id, team_id, source_id, target_id, source_handle, target_handle, relation_type, relation_category)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (source_id, target_id, relation_type) DO UPDATE SET
				source_handle = EXCLUDED.source_handle,
				target_handle = EXCLUDED.target_handle,
				team_id = EXCLUDED.team_id,
				relation_category = EXCLUDED.relation_category
		`, r.ID, r.TeamID, r.SourceID, r.TargetID, r.SourceHandle, r.TargetHandle, r.RelationType, r.RelationCategory)
		if err != nil {
			return fmt.Errorf("failed to insert agent relation: %w", err)
		}
	}

	return tx.Commit(ctx)
}

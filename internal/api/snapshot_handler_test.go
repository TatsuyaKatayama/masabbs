package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThreadSnapshotReplacePreservesMessagesAndLogs(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	insertSnapshotFixtures(t, ctx, db)

	e := echo.New()
	RegisterThreadSnapshotRoutes(e, db)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/export", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var snapshot models.ThreadSnapshot
	err := json.Unmarshal(rec.Body.Bytes(), &snapshot)
	require.NoError(t, err)
	require.Len(t, snapshot.Threads, 2)
	require.Len(t, snapshot.Tasks, 1)
	require.Len(t, snapshot.Logs, 1)
	assert.Equal(t, []string{"agent-2"}, snapshot.Tasks[0].ToAgents)
	assert.Equal(t, []string{"agent-3"}, snapshot.Tasks[0].Observers)

	_, err = db.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status, team_id) VALUES ('extra-thread','agent-1','open','team-1')`)
	require.NoError(t, err)

	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/threads/import", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var count int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM threads`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	var toAgents, observers []string
	err = db.QueryRow(ctx, `SELECT to_agents, observers FROM tasks WHERE id = 'task-1'`).Scan(&toAgents, &observers)
	require.NoError(t, err)
	assert.Equal(t, []string{"agent-2"}, toAgents)
	assert.Equal(t, []string{"agent-3"}, observers)

	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM logs`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestFullSnapshotReplaceIncludesSavedConfigs(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	insertSnapshotFixtures(t, ctx, db)
	_, err := db.Exec(ctx, `
		INSERT INTO configs (id, name, description, data)
		VALUES ('config-1', 'saved', 'saved config', '{"teams":[],"agents":[],"relations":[]}')
	`)
	require.NoError(t, err)

	e := echo.New()
	h := &Handler{DB: db}
	e.GET("/api/v1/snapshot/export", h.ExportSnapshot)
	e.POST("/api/v1/snapshot/import", h.ImportSnapshot)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshot/export", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var snapshot models.FullSnapshot
	err = json.Unmarshal(rec.Body.Bytes(), &snapshot)
	require.NoError(t, err)
	assert.Less(t, bytes.Index(rec.Body.Bytes(), []byte(`"agents"`)), bytes.Index(rec.Body.Bytes(), []byte(`"configs"`)))
	require.Len(t, snapshot.Configs, 1)
	require.Len(t, snapshot.Teams, 1)
	require.Len(t, snapshot.Agents, 3)
	require.Len(t, snapshot.Threads, 2)
	require.Len(t, snapshot.Tasks, 1)
	require.Len(t, snapshot.Logs, 1)

	_, err = db.Exec(ctx, `INSERT INTO configs (id, name, description, data) VALUES ('config-extra', 'extra', 'extra', '{}')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('team-extra','Extra')`)
	require.NoError(t, err)

	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/snapshot/import", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var count int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM configs`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM teams`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM threads`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestFullSnapshotImportAppliesEditedAgentFields(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	insertSnapshotFixtures(t, ctx, db)

	e := echo.New()
	h := &Handler{DB: db}
	e.GET("/api/v1/snapshot/export", h.ExportSnapshot)
	e.POST("/api/v1/snapshot/import", h.ImportSnapshot)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/snapshot/export", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var snapshot models.FullSnapshot
	err := json.Unmarshal(rec.Body.Bytes(), &snapshot)
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.Agents)

	for i := range snapshot.Agents {
		if snapshot.Agents[i].ID == "agent-1" {
			snapshot.Agents[i].Name = "Restored Agent One"
			snapshot.Agents[i].Mission = "restored mission"
			snapshot.Agents[i].Role = "worker"
			snapshot.Agents[i].Status = "busy"
		}
	}

	_, err = db.Exec(ctx, `
		UPDATE agents
		SET name = 'Changed After Export',
			mission = 'changed after export',
			role = 'manager',
			status = 'offline'
		WHERE id = 'agent-1'
	`)
	require.NoError(t, err)

	body, err := json.Marshal(snapshot)
	require.NoError(t, err)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/snapshot/import", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec = httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var name, mission, role, status string
	err = db.QueryRow(ctx, `SELECT name, mission, role, status FROM agents WHERE id = 'agent-1'`).Scan(&name, &mission, &role, &status)
	require.NoError(t, err)
	assert.Equal(t, "Restored Agent One", name)
	assert.Equal(t, "restored mission", mission)
	assert.Equal(t, "worker", role)
	assert.Equal(t, "busy", status)
}

func insertSnapshotFixtures(t *testing.T, ctx context.Context, db *pgxpool.Pool) {
	t.Helper()

	_, err := db.Exec(ctx, `INSERT INTO teams (id, name, description, mission) VALUES ('team-1','Team One','desc','mission')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO agents (id, name, role, mission, tools, capabilities, status, team_id, ui_pos_x, ui_pos_y)
		VALUES
			('agent-1','Agent One','manager','mission','[]','[]','offline','team-1',0,0),
			('agent-2','Agent Two','worker','mission','[]','[]','offline','team-1',1,1),
			('agent-3','Agent Three','observer','mission','[]','[]','offline','team-1',2,2)
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO team_agents (team_id, agent_id)
		VALUES
			('team-1','agent-1'),
			('team-1','agent-2'),
			('team-1','agent-3')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO agent_relations (id, team_id, source_id, target_id, relation_type, relation_category)
		VALUES ('rel-1','team-1','agent-1','agent-2','boss','vertical')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, assigned_agent, status, team_id)
		VALUES
			('thread-parent', NULL, 'agent-1', 'agent-2', 'open', 'team-1'),
			('thread-child', 'thread-parent', 'agent-2', NULL, 'processing', 'team-1')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload)
		VALUES ('task-1', 'thread-child', 'agent-1', 'task', ARRAY['agent-2'], ARRAY['agent-3'], '{"command":"do it"}')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO logs (id, thread_id, agent_id, level, message)
		VALUES (10, 'thread-child', 'agent-1', 'info', 'started')
	`)
	require.NoError(t, err)
}

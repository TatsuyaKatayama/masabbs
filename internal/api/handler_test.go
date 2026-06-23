package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupDB(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()
	pwd, _ := os.Getwd()
	schemaPath := filepath.Join(pwd, "..", "..", "internal", "db", "schema.sql")

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithInitScripts(schemaPath),
		tcpostgres.WithDatabase("masabbs-api-test"),
		tcpostgres.WithUsername("user"),
		tcpostgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(10*time.Second)),
	)
	require.NoError(t, err)

	dbURL, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	dbPool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)

	cleanup := func() {
		dbPool.Close()
		pgContainer.Terminate(ctx)
	}

	return dbPool, cleanup
}

func TestGenerateCredentials(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('team-a', 'Team A')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('worker-1', 'Worker 1', 'worker', 'team-a')`)
	require.NoError(t, err)

	authProvider, err := auth.NewProvider()
	require.NoError(t, err)

	e := echo.New()
	h := &Handler{
		DB:           db,
		AuthProvider: authProvider,
	}

	e.POST("/api/v1/agents/:id/credentials", h.GenerateCredentials)

	t.Run("Valid agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/worker-1/credentials", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusCreated, rec.Code)

		var creds auth.Credentials
		err := json.Unmarshal(rec.Body.Bytes(), &creds)
		require.NoError(t, err)

		assert.Equal(t, "worker-1", creds.AgentID)
		assert.NotEmpty(t, creds.NKeySeed)
		assert.NotEmpty(t, creds.JWT)
	})

	t.Run("Unknown agent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/unknown-999/credentials", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestTeamOrganizationAPI(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	// Setup Team
	_, err := db.Exec(ctx, "INSERT INTO teams (id, name, mission) VALUES ('t-complex', 'Complex Team', 'Solve everything')")
	require.NoError(t, err)

	// Setup Agents
	// x: Target of test, a: boss of x, b: staff of x, c: coworker of x, d: another coworker of x
	agents := []string{"x", "a", "b", "c", "d"}
	for _, id := range agents {
		_, err = db.Exec(ctx, "INSERT INTO agents (id, name, role, team_id, ui_pos_x, ui_pos_y) VALUES ($1, $2, 'worker', 't-complex', 0, 0)", id, "Agent "+id)
		require.NoError(t, err)
		_, err = db.Exec(ctx, "INSERT INTO team_agents (team_id, agent_id) VALUES ('t-complex', $1)", id)
		require.NoError(t, err)
	}

	// Setup Relations
	// 1. a is boss of x (a --[boss]--> x)
	_, err = db.Exec(ctx, "INSERT INTO agent_relations (id, team_id, source_id, target_id, relation_type, relation_category) VALUES ('r1', 't-complex', 'a', 'x', 'boss', 'vertical')")
	require.NoError(t, err)
	// 2. x is boss of b (x --[boss]--> b)
	_, err = db.Exec(ctx, "INSERT INTO agent_relations (id, team_id, source_id, target_id, relation_type, relation_category) VALUES ('r2', 't-complex', 'x', 'b', 'boss', 'vertical')")
	require.NoError(t, err)
	// 3. x and c are coworkers (x --[coworker]--> c)
	_, err = db.Exec(ctx, "INSERT INTO agent_relations (id, team_id, source_id, target_id, relation_type, relation_category) VALUES ('r3', 't-complex', 'x', 'c', 'coworker', 'horizontal')")
	require.NoError(t, err)
	// 4. d and x are coworkers (d --[coworker]--> x)
	_, err = db.Exec(ctx, "INSERT INTO agent_relations (id, team_id, source_id, target_id, relation_type, relation_category) VALUES ('r4', 't-complex', 'd', 'x', 'coworker', 'horizontal')")
	require.NoError(t, err)

	e := echo.New()
	h := &Handler{DB: db}

	t.Run("CreateTeam", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/teams", strings.NewReader(`{"name":"New Team","description":"new desc","mission":"new mission"}`))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		err := h.CreateTeam(e.NewContext(req, rec))
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, rec.Code)

		var team models.Team
		err = json.Unmarshal(rec.Body.Bytes(), &team)
		require.NoError(t, err)
		assert.NotEmpty(t, team.ID)
		assert.Equal(t, "New Team", team.Name)
		assert.Equal(t, "new mission", team.Mission)
	})

	t.Run("DeleteTeam cascades memberships and nulls thread team", func(t *testing.T) {
		_, err := db.Exec(ctx, "INSERT INTO teams (id, name) VALUES ('t-delete', 'Delete Me')")
		require.NoError(t, err)
		_, err = db.Exec(ctx, "INSERT INTO agents (id, name, role) VALUES ('delete-agent', 'Delete Agent', 'worker')")
		require.NoError(t, err)
		_, err = db.Exec(ctx, "INSERT INTO team_agents (team_id, agent_id) VALUES ('t-delete', 'delete-agent')")
		require.NoError(t, err)
		_, err = db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status, team_id) VALUES ('delete-thread', 'delete-agent', 'open', 't-delete')")
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/teams/t-delete", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/teams/:id")
		c.SetParamNames("id")
		c.SetParamValues("t-delete")

		err = h.DeleteTeam(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, rec.Code)

		var count int
		err = db.QueryRow(ctx, "SELECT COUNT(*) FROM team_agents WHERE team_id = 't-delete'").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		var teamID *string
		err = db.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = 'delete-thread'").Scan(&teamID)
		require.NoError(t, err)
		assert.Nil(t, teamID)
	})

	t.Run("GetAgentNetwork - Complex Mixed Relations for Agent X", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/x/network", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/agents/:id/network")
		c.SetParamNames("id")
		c.SetParamValues("x")

		err := h.GetAgentNetwork(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var network []models.NetworkMember
		err = json.Unmarshal(rec.Body.Bytes(), &network)
		require.NoError(t, err)

		// Should return 4 neighbors: a, b, c, d
		assert.Len(t, network, 4)

		// Map results for easier checking
		results := make(map[string]models.NetworkMember)
		for _, m := range network {
			results[m.AgentID] = m
		}

		// Verify Agent A: should be seen as BOSS (since a --[boss]--> x)
		assert.Equal(t, "boss", results["a"].Relation.Type)
		assert.Equal(t, "vertical", results["a"].Relation.Category)

		// Verify Agent B: should be seen as SUBORDINATE (since x --[boss]--> b)
		assert.Equal(t, "subordinate", results["b"].Relation.Type)
		assert.Equal(t, "vertical", results["b"].Relation.Category)

		// Verify Agent C: should be seen as COWORKER (horizontal is symmetric)
		assert.Equal(t, "coworker", results["c"].Relation.Type)
		assert.Equal(t, "horizontal", results["c"].Relation.Category)

		// Verify Agent D: should be seen as COWORKER (horizontal is symmetric)
		assert.Equal(t, "coworker", results["d"].Relation.Type)
		assert.Equal(t, "horizontal", results["d"].Relation.Category)
	})

	t.Run("GetTeamBlueprint", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/teams/t-complex/blueprint", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/teams/:id/blueprint")
		c.SetParamNames("id")
		c.SetParamValues("t-complex")

		err := h.GetTeamBlueprint(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var blueprint models.TeamBlueprint
		err = json.Unmarshal(rec.Body.Bytes(), &blueprint)
		require.NoError(t, err)

		assert.Equal(t, "t-complex", blueprint.TeamID)
		assert.Contains(t, blueprint.StructureMermaid, "graph TD")
		assert.Contains(t, blueprint.StructureMermaid, "a -- \"instruct\" --> x")
		assert.Contains(t, blueprint.StructureMermaid, "x -- \"instruct\" --> b")
		assert.Len(t, blueprint.Members, 5)
	})
}

func TestGetThreadContext(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	ctx := context.Background()
	_, err := db.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('team-context', 'Context Team')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-a', 'Agent A', 'manager', 'team-context')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-b', 'Agent B', 'worker', 'team-context')`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, created_by_agent, status, team_id, created_at)
		VALUES ('thread-root', 'agent-a', 'open', 'team-context', '2026-06-24T00:00:00Z')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status, team_id, created_at)
		VALUES ('thread-child', 'thread-root', 'agent-b', 'open', 'team-context', '2026-06-24T00:05:00Z')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload, created_at)
		VALUES
			('task-root', 'thread-root', 'agent-a', 'task', ARRAY['agent-b'], ARRAY[]::TEXT[], '{"command":"root"}', '2026-06-24T00:01:00Z'),
			('task-child', 'thread-child', 'agent-b', 'task', ARRAY['agent-a'], ARRAY[]::TEXT[], '{"command":"child"}', '2026-06-24T00:02:00Z'),
			('task-result', 'thread-child', 'agent-b', 'result', ARRAY['agent-a'], ARRAY[]::TEXT[], '{"message":"done"}', '2026-06-24T00:03:00Z')
	`)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO logs (thread_id, agent_id, level, message, created_at)
		VALUES
			('thread-root', 'agent-a', 'info', 'root log', '2026-06-24T00:01:30Z'),
			('thread-child', 'agent-b', 'info', 'child log', '2026-06-24T00:02:30Z')
	`)
	require.NoError(t, err)

	e := echo.New()
	h := &Handler{DB: db}
	e.GET("/api/v1/threads/:id/context", h.GetThreadContext)

	t.Run("root thread only by default", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-root/context", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp ThreadContextResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, "thread-root", resp.Thread.ID)
		assert.False(t, resp.Options.IncludeSubthreads)
		assert.Len(t, resp.Threads, 1)
		assert.Len(t, resp.Tasks, 1)
		assert.Equal(t, "task-root", resp.Tasks[0].ID)
		assert.Len(t, resp.Logs, 1)
		assert.Equal(t, "root log", resp.Logs[0].Message)
	})

	t.Run("includes subthreads with ordering and limit", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-root/context?include_subthreads=true&order=desc&message_limit=2", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var resp ThreadContextResponse
		err := json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.True(t, resp.Options.IncludeSubthreads)
		assert.Equal(t, "desc", resp.Options.Order)
		assert.Equal(t, 2, resp.Options.MessageLimit)
		assert.Len(t, resp.Threads, 2)
		assert.Len(t, resp.Tasks, 2)
		assert.Equal(t, "task-result", resp.Tasks[0].ID)
		assert.Equal(t, "task-child", resp.Tasks[1].ID)
		assert.Len(t, resp.Logs, 2)
		assert.Equal(t, "child log", resp.Logs[0].Message)
		assert.Equal(t, "root log", resp.Logs[1].Message)
	})

	t.Run("unknown thread returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/missing/context", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("invalid query returns 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/thread-root/context?order=sideways", nil)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHealthCheck(t *testing.T) {
	db, cleanup := setupDB(t)
	defer cleanup()

	e := echo.New()
	h := &Handler{DB: db}

	t.Run("HealthCheck success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		err := h.HealthCheck(c)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, rec.Code)

		var resp map[string]string
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
	})
}

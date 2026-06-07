package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

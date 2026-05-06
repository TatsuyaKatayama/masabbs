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

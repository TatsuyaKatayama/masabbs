package db_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()

	// Locate schema.sql
	pwd, err := os.Getwd()
	require.NoError(t, err)
	schemaPath := filepath.Join(pwd, "schema.sql")

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithInitScripts(schemaPath),
		postgres.WithDatabase("masabbs-test"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	// Ping database
	err = pool.Ping(ctx)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate pgContainer: %s", err)
		}
	}

	return pool, cleanup
}

func TestDBIntegrity(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// Pre-populate agents and teams for constraints
	_, err := pool.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('team-1', 'Test Team')`)
	require.NoError(t, err)
	
	_, err = pool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-1', 'Test Agent', 'worker', 'team-1')`)
	require.NoError(t, err)

	t.Run("UT-DB-003: Concurrent INSERT conflict", func(t *testing.T) {
		threadID := "thread-db-003"
		
		// First insert should succeed
		_, err := pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'open')`, threadID, "agent-1")
		require.NoError(t, err)

		// Second insert with same ID should fail (409 Conflict logic equivalent in DB is PK violation)
		_, err = pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'open')`, threadID, "agent-1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "duplicate key value violates unique constraint")
	})

	t.Run("UT-DB-004: Transaction rollback on failure", func(t *testing.T) {
		threadID := "thread-db-004"
		
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)

		_, err = tx.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'open')`, threadID, "agent-1")
		require.NoError(t, err)

		// Cause an error on purpose (invalid foreign key)
		_, err = tx.Exec(ctx, `INSERT INTO tasks (id, thread_id, agent_id, type, payload) VALUES ('task-1', $1, 'invalid-agent', 'task', '{}')`, threadID)
		require.Error(t, err)
		
		err = tx.Rollback(ctx)
		require.NoError(t, err)

		// Verify thread was rolled back
		var count int
		err = pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE id = $1`, threadID).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

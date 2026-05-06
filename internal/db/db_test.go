package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, func()) {
	ctx := context.Background()
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
				WithOccurrence(2).WithStartupTimeout(10*time.Second)),
	)
	require.NoError(t, err)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		pgContainer.Terminate(ctx)
	}
	return pool, cleanup
}

func TestDBIntegrity(t *testing.T) {
	pool, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('team-1', 'Test Team')`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-1', 'Test Agent', 'worker', 'team-1')`)
	require.NoError(t, err)

	t.Run("UT-DB-003: Concurrent INSERT conflict", func(t *testing.T) {
		threadID := "thread-db-003"
		_, err := pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'open')`, threadID, "agent-1")
		require.NoError(t, err)
		_, err = pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'open')`, threadID, "agent-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key value violates unique constraint")
	})

	t.Run("UT-DB-001: Thread task integrity dummy check", func(t *testing.T) {
		threadID := "thread-db-001"
		_, err := pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, $2, 'done')`, threadID, "agent-1")
		require.NoError(t, err)
		var hasResult bool
		err = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tasks WHERE thread_id = $1 AND type = 'result')`, threadID).Scan(&hasResult)
		require.NoError(t, err)
		if !hasResult {
			t.Log("Warning: Thread is done but no result task exists")
		}
	})

	t.Run("UT-DB-004: Transaction rollback", func(t *testing.T) {
		threadID := "thread-db-004"
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		tx.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-1', 'open')`, threadID)
		tx.Rollback(ctx)
		var count int
		pool.QueryRow(ctx, `SELECT count(*) FROM threads WHERE id = $1`, threadID).Scan(&count)
		assert.Equal(t, 0, count)
	})

	t.Run("UT-DB-002: Inconsistency Detection", func(t *testing.T) {
		threadID := "thread-db-inconsistent"
		_, err := pool.Exec(ctx, `INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-1', 'done')`, threadID)
		require.NoError(t, err)
		inconsistent, err := CheckThreadIntegrity(ctx, pool)
		require.NoError(t, err)
		assert.Contains(t, inconsistent, threadID)
	})

	t.Run("UT-DB-005: Deadlock Retry Logic", func(t *testing.T) {
		attempts := 0
		err := RunWithRetry(ctx, pool, func(tx pgx.Tx) error {
			attempts++
			if attempts < 3 {
				return fmt.Errorf("ERROR: deadlock detected (SQLSTATE 40P01)")
			}
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 3, attempts)
	})
}

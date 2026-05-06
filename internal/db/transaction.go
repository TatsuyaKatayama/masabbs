package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunWithRetry executes a transaction and retries if a deadlock (40P01) or serialization error occurs.
func RunWithRetry(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	const maxRetries = 3
	var err error

	for i := 0; i < maxRetries; i++ {
		err = executeTx(ctx, pool, fn)
		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "40001") || strings.Contains(err.Error(), "40P01") {
			time.Sleep(time.Duration(10*(i+1)) * time.Millisecond) // Exponential backoff
			continue
		}

		return err
	}
	return fmt.Errorf("transaction failed after %d retries: %w", maxRetries, err)
}

func executeTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// CheckThreadIntegrity verifies that closed threads have valid terminal tasks (UT-DB-002).
func CheckThreadIntegrity(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT t.id 
		FROM threads t
		LEFT JOIN tasks tk ON t.id = tk.thread_id AND tk.type = 'result'
		WHERE t.status = 'done' AND tk.id IS NULL
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var inconsistentThreads []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		inconsistentThreads = append(inconsistentThreads, id)
	}

	return inconsistentThreads, nil
}

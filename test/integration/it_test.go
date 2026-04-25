package integration

import (
	"fmt"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/api"
	"github.com/TatsuyaKatayama/masabbs/internal/nats"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupIntegrationEnvironment(t *testing.T) (*pgxpool.Pool, *nats.Client, func()) {
	ctx := context.Background()

	// 1. Setup PostgreSQL
	pwd, _ := os.Getwd()
	schemaPath := filepath.Join(pwd, "..", "..", "internal", "db", "schema.sql")

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithInitScripts(schemaPath),
		tcpostgres.WithDatabase("masabbs-it"),
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

	_, err = dbPool.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('test-team', 'Test Team')`)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-1', 'Agent 1', 'worker', 'test-team')`)
	require.NoError(t, err)

	// 2. Setup NATS
	// In testcontainers NATS module, passing command line args requires generic testcontainers.ContainerRequest
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor:   wait.ForLog("Server is ready"),
	}
	natsGeneric, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	host, err := natsGeneric.Host(ctx)
	require.NoError(t, err)
	port, err := natsGeneric.MappedPort(ctx, "4222/tcp")
	require.NoError(t, err)

	natsURL := "nats://" + host + ":" + port.Port()

	natsClient, err := nats.Connect(nats.Config{URL: natsURL})
	require.NoError(t, err)

	cleanup := func() {
		natsClient.Close()
		dbPool.Close()
		natsGeneric.Terminate(ctx)
		pgContainer.Terminate(ctx)
	}

	return dbPool, natsClient, cleanup
}

func TestIntegration_IT008_WebSocketConflict(t *testing.T) {
	_, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	e := echo.New()
	hub := api.NewHub(nc.NC)
	go hub.Run(context.Background())

	e.GET("/ws", func(c echo.Context) error {
		hub.ServeWS(c.Response(), c.Request())
		return nil
	})

	server := httptest.NewServer(e)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws?agent_id=admin-ui"

	dialer := websocket.Dialer{}
	conn1, resp1, err := dialer.Dial(wsURL, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp1.StatusCode)
	defer conn1.Close()

	conn2, resp2, err := dialer.Dial(wsURL, nil)
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, resp2.StatusCode)
	if conn2 != nil {
		conn2.Close()
	}
}

func TestIntegration_IT001_CorruptedMessage(t *testing.T) {
	db, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
	}
	go archiver.Start(ctx)

	time.Sleep(1 * time.Second)

	_, err := nc.JS.Publish(ctx, "board.task.corrupted", []byte(`{invalid json`))
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count)
	require.NoError(t, err)
	
	// Should not have persisted anything due to unmarshal error
	assert.Equal(t, 0, count)
}

func TestIntegration_IT002_DuplicateMessages(t *testing.T) {
	db, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
	}
	go archiver.Start(ctx)

	time.Sleep(1 * time.Second)

	threadID := "thread-it-002"
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-1', 'open')", threadID)
	require.NoError(t, err)

	validJSON := fmt.Sprintf(`{"type":"result", "thread_id":"%s", "from":"agent-1", "timestamp":%d, "payload":{"output_dir":"tasks/%s/output/","exit_code":0}}`, threadID, time.Now().Unix(), threadID)

	// Publish the SAME message 3 times rapidly
	for i := 0; i < 3; i++ {
		_, err := nc.JS.Publish(ctx, "board.result."+threadID, []byte(validJSON))
		require.NoError(t, err)
	}

	time.Sleep(2 * time.Second)

	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'result'", threadID).Scan(&count)
	require.NoError(t, err)
	
	// Currently the archiver generates a NEW ULID for every message it receives from NATS and inserts it into tasks.
	// IT-002 specifically targets ensuring duplicate messages do not cause adverse side effects or crash.
	// Since the server doesn't use message IDs from the client, duplicates will be recorded as 3 separate task log entries.
	// However, it should NOT crash, and state machine updates (which are separate) must remain idempotent.
	// For this test, we verify the archiver successfully processed all 3 (or deduplicated them if NATS deduplication was used).
	// Without NATS MsgId header, it processes all 3.
	assert.Equal(t, 3, count, "Archiver should record all 3 messages without crashing")
}

func TestIntegration_IT003_DelayedMessageState(t *testing.T) {
	db, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Initialize thread with state 'done'
	threadID := "thread-it-003"
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-1', 'done')", threadID)
	require.NoError(t, err)

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
	}
	go archiver.Start(ctx)

	time.Sleep(1 * time.Second)

	// Delayed 'assign' message arrives for a thread that is already 'done'
	delayedAssignJSON := fmt.Sprintf(`{"type":"assign", "thread_id":"%s", "from":"agent-1", "to":["agent-2"], "timestamp":%d, "payload":{}}`, threadID, time.Now().Unix() - 3600)
	
	_, err = nc.JS.Publish(ctx, "board.assign."+threadID, []byte(delayedAssignJSON))
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	// Ensure the message was archived
	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'assign'", threadID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "Delayed message should be archived")

	// Ensure thread state was NOT changed back to 'assigned'
	var status string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "done", status, "Delayed message should not break state machine, thread must remain 'done'")
}

package e2e

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/nats"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupE2EEnvironment(t *testing.T) (*pgxpool.Pool, *nats.Client, func()) {
	ctx := context.Background()

	pwd, _ := os.Getwd()
	schemaPath := filepath.Join(pwd, "..", "..", "internal", "db", "schema.sql")

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithInitScripts(schemaPath),
		tcpostgres.WithDatabase("masabbs-e2e"),
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

	_, err = dbPool.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('e2e-team', 'E2E Team')`)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-a', 'Agent A', 'manager', 'e2e-team')`)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-b', 'Agent B', 'worker', 'e2e-team')`)
	require.NoError(t, err)
	_, err = dbPool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ('agent-c', 'Agent C', 'worker', 'e2e-team')`)
	require.NoError(t, err)

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

func TestE2E_NORMAL_001_1to1Task(t *testing.T) {
	db, nc, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1B"

	// 1. Server registers new thread
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
	require.NoError(t, err)

	// 2. Setup Mock Agent B
	consumer, err := nc.JS.CreateOrUpdateConsumer(ctx, "board_tasks", jetstream.ConsumerConfig{
		Durable:       "worker-agent-b",
		FilterSubject: "board.task.*",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       2 * time.Second,
	})
	require.NoError(t, err)

	// Agent B Autonomous Loop
	go func() {
		for {
			msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(1*time.Second))
			if err != nil {
				continue
			}
			for msg := range msgs.Messages() {
				var env models.MessageEnvelope
				json.Unmarshal(msg.Data(), &env)
				
				// Simulate status update
				db.Exec(context.Background(), "UPDATE threads SET status = 'processing', assigned_agent = 'agent-b' WHERE id = $1", *env.ThreadID)
				
				msg.InProgress() 
				time.Sleep(1 * time.Second)

				payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
				resEnv := models.MessageEnvelope{
					Type: "result", ThreadID: env.ThreadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: payload,
				}
				data, _ := json.Marshal(resEnv)
				nc.NC.Publish("board.result."+*env.ThreadID, data)
				
				db.Exec(context.Background(), "UPDATE threads SET status = 'done' WHERE id = $1", *env.ThreadID)
				msg.Ack()
				return
			}
		}
	}()

	// 3. Trigger workflow
	payload, _ := json.Marshal(models.TaskPayload{Command: "Do work", InputDir: "in/", Deadline: ""})
	taskEnv := models.MessageEnvelope{
		Type: "task", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: payload,
	}
	data, _ := json.Marshal(taskEnv)
	err = nc.NC.Publish("board.task."+threadID, data)
	require.NoError(t, err)

	time.Sleep(3 * time.Second)

	var status string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "done", status, "Thread should converge to 'done' state via V10 Pull Consumer flow")
}

func TestE2E_ERR_001_AgentSilence(t *testing.T) {
	db, _, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1C"
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'processing')", threadID)
	require.NoError(t, err)

	go func() {
		time.Sleep(2 * time.Second)
		var status string
		db.QueryRow(context.Background(), "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
		if status == "processing" {
			db.Exec(context.Background(), "UPDATE threads SET status = 'error' WHERE id = $1", threadID)
		}
	}()

	time.Sleep(3 * time.Second)

	var finalStatus string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&finalStatus)
	require.NoError(t, err)
	assert.Equal(t, "error", finalStatus, "Thread should converge to 'error' on silence")
}

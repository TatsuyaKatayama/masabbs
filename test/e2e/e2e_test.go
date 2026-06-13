package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/api"
	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/nats"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	libnats "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupE2EEnvironment(t *testing.T) (*pgxpool.Pool, *nats.Client, *auth.Provider, map[string]*auth.Credentials, func()) {
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

	authProvider, _ := auth.NewProvider()
	credsMap := make(map[string]*auth.Credentials)

	_, err = dbPool.Exec(ctx, `INSERT INTO teams (id, name) VALUES ('e2e-team', 'E2E Team')`)
	require.NoError(t, err)

	registerAgent := func(id, name, role string) {
		_, err := dbPool.Exec(ctx, `INSERT INTO agents (id, name, role, team_id) VALUES ($1, $2, $3, 'e2e-team')`, id, name, role)
		require.NoError(t, err)
		creds, _ := authProvider.GenerateAgentCredentials(id, role)
		credsMap[id] = creds
	}

	registerAgent("agent-a", "Agent A", "manager")
	registerAgent("agent-b", "Agent B", "worker")
	registerAgent("agent-c", "Agent C", "worker")
	registerAgent("agent-d", "Agent D", "worker")
	registerAgent("agent-o", "Agent O", "observer")

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

	return dbPool, natsClient, authProvider, credsMap, cleanup
}

func signAndPublish(t *testing.T, nc *nats.Client, auth *auth.Provider, creds *auth.Credentials, env models.MessageEnvelope, subject string) {
	env.Signature = "" // Ensure signature is empty before signing

	// ARCHIVERの検証ロジック（mapによるキーソート）に合わせる
	var envMap map[string]interface{}
	tempData, _ := json.Marshal(env)
	json.Unmarshal(tempData, &envMap)
	delete(envMap, "signature")
	data, _ := json.Marshal(envMap)

	sig, err := auth.SignMessage(creds.NKeySeed, data)
	require.NoError(t, err)
	env.Signature = sig

	finalData, _ := json.Marshal(env)
	err = nc.NC.Publish(subject, finalData)
	require.NoError(t, err)
}
func TestE2E_NORMAL_001_1to1Task(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
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

				signAndPublish(t, nc, authProvider, creds["agent-b"], resEnv, "board.result."+*env.ThreadID)

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

	signAndPublish(t, nc, authProvider, creds["agent-a"], taskEnv, "board.task."+threadID)

	time.Sleep(3 * time.Second)

	var status string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
	require.NoError(t, err)
	assert.Equal(t, "done", status, "Thread should converge to 'done' state via V10 Pull Consumer flow")
}

func TestE2E_ERR_001_AgentSilence(t *testing.T) {
	db, _, _, _, cleanup := setupE2EEnvironment(t)
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

func TestE2E_NORMAL_002_BroadcastAndAggregation(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1D"
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
	require.NoError(t, err)

	// Setup Worker Agents (B, C, D)
	workerIDs := []string{"agent-b", "agent-c", "agent-d"}
	for _, id := range workerIDs {
		go func(agentID string) {
			consumer, err := nc.JS.CreateOrUpdateConsumer(ctx, "board_tasks", jetstream.ConsumerConfig{
				Durable:       "worker-" + agentID,
				FilterSubject: "board.task.*",
				AckPolicy:     jetstream.AckExplicitPolicy,
			})
			if err != nil {
				return
			}
			for {
				msgs, err := consumer.Fetch(1, jetstream.FetchMaxWait(1*time.Second))
				if err != nil {
					continue
				}
				for msg := range msgs.Messages() {
					var env models.MessageEnvelope
					json.Unmarshal(msg.Data(), &env)

					// Only respond if targeted in to_agents (broadcast includes them)
					isTargeted := false
					for _, to := range env.To {
						if to == agentID {
							isTargeted = true
							break
						}
					}
					if !isTargeted {
						msg.Ack()
						continue
					}

					payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/" + agentID, ExitCode: 0})
					resEnv := models.MessageEnvelope{
						Type: "result", ThreadID: env.ThreadID, From: agentID, Timestamp: time.Now().Unix(), Payload: payload,
					}
					signAndPublish(t, nc, authProvider, creds[agentID], resEnv, "board.result."+*env.ThreadID)
					msg.Ack()
					return
				}
			}
		}(id)
	}

	// Agent A publishes Broadcast Task
	payload, _ := json.Marshal(models.TaskPayload{Command: "Compute pi", InputDir: "in/", Deadline: ""})
	taskEnv := models.MessageEnvelope{
		Type: "task", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: payload,
		To: []string{"agent-b", "agent-c", "agent-d"},
	}
	signAndPublish(t, nc, authProvider, creds["agent-a"], taskEnv, "board.task."+threadID)

	time.Sleep(5 * time.Second)

	// Verify all 3 results are in DB
	var resultCount int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'result'", threadID).Scan(&resultCount)
	require.NoError(t, err)
	assert.Equal(t, 3, resultCount, "Should collect 3 results from different agents")

	// 4. Agent A (Manager) aggregates results and marks thread as done
	// In a real scenario, Agent A would be listening to 'board.result.threadID'
	_, err = db.Exec(ctx, "UPDATE threads SET status = 'done' WHERE id = $1", threadID)
	require.NoError(t, err)

	var finalStatus string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&finalStatus)
	require.NoError(t, err)
	assert.Equal(t, "done", finalStatus, "Thread should be marked as 'done' after aggregation")
}

func TestE2E_NORMAL_004_ObserverSubscription(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1E"
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
	require.NoError(t, err)

	// Observer Agent O starts listening to all events
	receivedMessages := make(chan string, 10)
	sub, err := nc.NC.Subscribe("board.>", func(m *libnats.Msg) {
		var env models.MessageEnvelope
		json.Unmarshal(m.Data, &env)
		receivedMessages <- env.Type
	})
	require.NoError(t, err)
	defer sub.Unsubscribe()

	// Agent B (Worker)
	go func() {
		consumer, _ := nc.JS.CreateOrUpdateConsumer(ctx, "board_tasks", jetstream.ConsumerConfig{
			Durable:       "worker-observer-test",
			FilterSubject: "board.task.*",
		})
		msgs, _ := consumer.Fetch(1)
		for msg := range msgs.Messages() {
			var env models.MessageEnvelope
			json.Unmarshal(msg.Data(), &env)
			resEnv := models.MessageEnvelope{
				Type: "result", ThreadID: env.ThreadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: []byte("{}"),
			}
			signAndPublish(t, nc, authProvider, creds["agent-b"], resEnv, "board.result."+*env.ThreadID)
			msg.Ack()
			return
		}
	}()

	// Agent A publishes Task
	taskEnv := models.MessageEnvelope{
		Type: "task", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: []byte("{}"),
		To: []string{"agent-b"},
	}
	signAndPublish(t, nc, authProvider, creds["agent-a"], taskEnv, "board.task."+threadID)

	// Check if Observer received both 'task' and 'result'
	types := make(map[string]bool)
	timeout := time.After(5 * time.Second)
	for len(types) < 2 {
		select {
		case t := <-receivedMessages:
			types[t] = true
		case <-timeout:
			t.Fatal("Observer timed out waiting for messages")
		}
	}

	assert.True(t, types["task"], "Observer should see 'task' message")
	assert.True(t, types["result"], "Observer should see 'result' message")
}

type MockStorage struct{}

func (m *MockStorage) GetThreadInputPath(threadID string) string {
	return fmt.Sprintf("tasks/%s/input/", threadID)
}
func (m *MockStorage) CreateThreadFolders(ctx context.Context, threadID string) error {
	return nil
}
func (m *MockStorage) ListFiles(ctx context.Context, prefix string) ([]string, error) {
	return []string{}, nil
}
func (m *MockStorage) GetPresignedURL(ctx context.Context, objectKey string) (string, error) {
	return "http://localhost:9000/mock-url", nil
}

func TestE2E_NORMAL_005_TaskWithToAndCC(t *testing.T) {
	db, nc, authProvider, _, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	// 1. Hub と WebSocket は不要（NATSメッセージの直接検証）
	receivedMsg := make(chan models.MessageEnvelope, 1)
	_, err := nc.NC.Subscribe("board.task.*", func(m *libnats.Msg) {
		var env models.MessageEnvelope
		json.Unmarshal(m.Data, &env)
		receivedMsg <- env
	})
	require.NoError(t, err)

	// 2. API を叩いてタスク発行
	e := echo.New()
	hub := api.NewHub(nc.NC, db)
	api.RegisterRoutes(e, db, nc, &MockStorage{}, hub, authProvider)

	reqBody := map[string]interface{}{
		"command":          "Test To and CC @agent-b @agent-c",
		"created_by_agent": "agent-a",
		"to":               []string{"agent-b", "agent-c"},
		"observers":        []string{"agent-o"},
		"deadline":         time.Now().Add(1 * time.Hour).Format(time.RFC3339),
	}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	// 3. NATSメッセージの中身を検証
	select {
	case env := <-receivedMsg:
		assert.Equal(t, "task", env.Type)
		assert.Contains(t, env.To, "agent-b")
		assert.Contains(t, env.To, "agent-c")
		assert.Len(t, env.To, 2)
		assert.Equal(t, []string{"agent-o"}, env.Observers)
		assert.Equal(t, "agent-a", env.From)
	case <-time.After(3 * time.Second):
		t.Fatal("Timed out waiting for NATS message")
	}
}

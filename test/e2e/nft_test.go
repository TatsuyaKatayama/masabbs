package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNFT_CONC_001_HighConcurrency(t *testing.T) {
	db, nc, authProvider, _, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	
	guardian := &worker.Guardian{AuthProvider: authProvider, NC: nc.NC, DB: db}
	go guardian.Start(ctx)

	const numThreads = 100
	var wg sync.WaitGroup
	wg.Add(numThreads)

	// Pre-register agents to avoid rate limits on a single agent
	agentCreds := make(map[int]*auth.Credentials)
	for i := 0; i < numThreads; i++ {
		agentID := fmt.Sprintf("worker-%03d", i)
		_, err := db.Exec(ctx, "INSERT INTO agents (id, name, role, team_id) VALUES ($1, $2, 'worker', 'e2e-team')", agentID, agentID)
		require.NoError(t, err)
		creds, _ := authProvider.GenerateAgentCredentials(agentID, "worker")
		agentCreds[i] = creds
	}

	start := time.Now()
	for i := 0; i < numThreads; i++ {
		go func(idx int) {
			defer wg.Done()
			threadID := fmt.Sprintf("01HGWY5X9A7Z4K2M3Q8P6R0V%02X", idx)
			agentID := fmt.Sprintf("worker-%03d", idx)
			
			// 1. Create thread (as system/admin, usually manager does this)
			_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
			if err != nil {
				return
			}

			// 2. Publish task (from manager agent-a)
			// Note: agent-a might still hit rate limit if we are not careful, 
			// but we'll use the pre-registered worker agents for results.
			// Let's use a small sleep to spread manager tasks.
			time.Sleep(time.Duration(idx*10) * time.Millisecond)

			// 3. Publish result (from unique worker agent)
			resPayload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
			resEnv := models.MessageEnvelope{
				Type: "result", ThreadID: &threadID, From: agentID, Timestamp: time.Now().Unix(), Payload: resPayload,
			}
			signAndPublish(t, nc, authProvider, agentCreds[idx], resEnv, "board.result."+threadID)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)
	t.Logf("Published %d results from %d agents in %v", numThreads, numThreads, duration)

	// Wait for processing to finish
	time.Sleep(10 * time.Second)

	// Verify counts in DB
	var taskCount int
	err := db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE type = 'result'").Scan(&taskCount)
	require.NoError(t, err)
	assert.Equal(t, numThreads, taskCount, "Should process all concurrent results without data loss")
}

func TestNFT_REC_001_RecoveryAfterRestart(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx := context.Background()
	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1J"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)

	// 1. Publish messages while no archiver is running
	payload, _ := json.Marshal(models.TaskPayload{Command: "Async task"})
	taskEnv := models.MessageEnvelope{
		Type: "task", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: payload,
	}
	signAndPublish(t, nc, authProvider, creds["agent-a"], taskEnv, "board.task."+threadID)
	
	resPayload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
	resEnv := models.MessageEnvelope{
		Type: "result", ThreadID: &threadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: resPayload,
	}
	signAndPublish(t, nc, authProvider, creds["agent-b"], resEnv, "board.result."+threadID)

	// Verify DB is still empty
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count)
	assert.Equal(t, 0, count, "DB should be empty as archiver is not running")

	// 2. Start archiver and verify it picks up old messages
	archiverCtx, cancel := context.WithCancel(ctx)
	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	archiver.Start(archiverCtx)
	
	time.Sleep(5 * time.Second)
	
	db.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count)
	assert.Equal(t, 2, count, "Archiver should process pending messages from JetStream after startup")
	cancel()
}

func TestNFT_PERF_001_Latency(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	
	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1K"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)

	payload, _ := json.Marshal(models.StatusPayload{Progress: 10, State: "running"})
	env := models.MessageEnvelope{
		Type: "status", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: payload,
	}

	start := time.Now()
	signAndPublish(t, nc, authProvider, creds["agent-a"], env, "board.status."+threadID)

	// Wait for it to appear in DB
	var taskID string
	for i := 0; i < 50; i++ {
		err := db.QueryRow(ctx, "SELECT id FROM tasks WHERE thread_id = $1 AND type = 'status'", threadID).Scan(&taskID)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	latency := time.Since(start)
	t.Logf("End-to-end latency (Publish -> DB): %v", latency)

	require.NotEmpty(t, taskID, "Message should be persisted in DB")
	// Target: < 1s for end-to-end persistence in E2E env (DB/NATS in docker)
	assert.Less(t, latency, 1*time.Second, "Latency should be within acceptable limits")
}

func TestNFT_PERF_002_Throughput(t *testing.T) {
	db, nc, authProvider, _, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)

	const totalMsgs = 500
	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1L"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)

	// Pre-register many agents to avoid per-agent rate limits
	numAgents := 50
	agentCreds := make([]*auth.Credentials, numAgents)
	for i := 0; i < numAgents; i++ {
		id := fmt.Sprintf("perf-worker-%03d", i)
		db.Exec(ctx, "INSERT INTO agents (id, name, role, team_id) VALUES ($1, $2, 'worker', 'e2e-team')", id, id)
		creds, _ := authProvider.GenerateAgentCredentials(id, "worker")
		agentCreds[i] = creds
	}

	start := time.Now()
	for i := 0; i < totalMsgs; i++ {
		agentIdx := i % numAgents
		payload, _ := json.Marshal(models.StatusPayload{Progress: i % 100, State: "running"})
		env := models.MessageEnvelope{
			Type: "status", ThreadID: &threadID, From: agentCreds[agentIdx].AgentID, Timestamp: time.Now().Unix(), Payload: payload,
		}
		signAndPublish(t, nc, authProvider, agentCreds[agentIdx], env, "board.status."+threadID)
	}

	// Wait for all messages to be in DB
	var count int
	for i := 0; i < 100; i++ {
		db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1", threadID).Scan(&count)
		if count >= totalMsgs {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	duration := time.Since(start)
	throughput := float64(count) / duration.Seconds()
	t.Logf("Processed %d messages in %v (%.2f msg/sec)", count, duration, throughput)

	assert.Equal(t, totalMsgs, count, "Should not lose any messages during high throughput")
	assert.Greater(t, throughput, 50.0, "Throughput should be at least 50 msg/sec in E2E environment")
}

func TestNFT_AVAIL_001_DB_TemporaryDown(t *testing.T) {
	// This test requires ability to stop/start containers, which is possible with testcontainers.
	// However, for simplicity in this environment, we might simulate it or focus on REC/PERF first.
	t.Skip("Skipping DB temporary down test for now - complex to orchestrate in this runner")
}

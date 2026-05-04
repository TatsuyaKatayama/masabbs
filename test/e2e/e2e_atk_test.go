package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_ATK_001_RateLimit(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
		Auth: authProvider,
	}
	go archiver.Start(ctx)

	guardian := &worker.Guardian{
		AuthProvider: authProvider,
		NC:           nc.NC,
	}
	go guardian.Start(ctx)

	agentID := "agent-b"
	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1D"
	
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
	require.NoError(t, err)

	payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
	resEnv := models.MessageEnvelope{
		Type: "result", ThreadID: &threadID, From: agentID, Timestamp: time.Now().Unix(), Payload: payload,
	}
	subject := "board.result." + threadID

	// 70通パブリッシュ
	for i := 0; i < 70; i++ {
		signAndPublish(t, nc, authProvider, creds[agentID], resEnv, subject)
	}

	// アーカイバーの処理を待機
	time.Sleep(5 * time.Second)

	// DBに保存された件数を確認
	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE agent_id = $1", agentID).Scan(&count)
	require.NoError(t, err)

	t.Logf("Messages in DB: %d", count)
	assert.LessOrEqual(t, count, 61, "Should be limited around 60 (actually likely around 6-10 due to per-sec limit)")
}

func TestE2E_ATK_004_IdImpersonation(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
		Auth: authProvider,
	}
	go archiver.Start(ctx)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1F"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)

	// agent-b tries to impersonate agent-a
	// env.From = 'agent-a', but signed with creds["agent-b"].NKeySeed
	payload, _ := json.Marshal(models.TaskPayload{Command: "Malicious work"})
	env := models.MessageEnvelope{
		Type: "task", ThreadID: &threadID, From: "agent-a", Timestamp: time.Now().Unix(), Payload: payload,
	}
	
	// Prepare canonical data for signing
	data, _ := json.Marshal(env)
	sig, _ := authProvider.SignMessage(creds["agent-b"].NKeySeed, data)
	env.Signature = sig
	
	finalData, _ := json.Marshal(env)
	nc.NC.Publish("board.task."+threadID, finalData)

	time.Sleep(3 * time.Second)

	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND agent_id = 'agent-a'", threadID).Scan(&count)
	assert.Equal(t, 0, count, "Impersonated message should NOT be persisted")
}

func TestE2E_ATK_005_UnauthorizedShutdown(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
		Auth: authProvider,
	}
	go archiver.Start(ctx)

	// agent-b (worker) tries to send shutdown
	payload, _ := json.Marshal(models.ShutdownPayload{Reason: "I am evil"})
	env := models.MessageEnvelope{
		Type: "shutdown", From: "agent-b", Timestamp: time.Now().Unix(), Payload: payload,
	}
	
	signAndPublish(t, nc, authProvider, creds["agent-b"], env, "board.shutdown")

	time.Sleep(3 * time.Second)

	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE type = 'shutdown' AND agent_id = 'agent-b'").Scan(&count)
	assert.Equal(t, 0, count, "Unauthorized shutdown should NOT be persisted")
}

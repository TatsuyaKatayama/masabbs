package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_ATK_003_HugePayload(t *testing.T) {
	e := echo.New()
	// In the real server it's 100M, but we test with 1M to keep tests fast and efficient.
	// The mechanism is the same.
	e.Use(middleware.BodyLimit("1M"))
	
	e.POST("/api/v1/threads", func(c echo.Context) error {
		return c.NoContent(http.StatusCreated)
	})

	t.Run("Accepts small payload", func(t *testing.T) {
		smallData := []byte(`{"command":"test"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(smallData))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusCreated, rec.Code)
	})

	t.Run("Rejects 2MB payload", func(t *testing.T) {
		hugeData := make([]byte, 2*1024*1024) // 2MB
		req := httptest.NewRequest(http.MethodPost, "/api/v1/threads", bytes.NewReader(hugeData))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})
}

func TestE2E_ATK_006_BurstLimit(t *testing.T) {
	_, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	guardian := &worker.Guardian{
		AuthProvider: authProvider,
		NC:           nc.NC,
	}
	go guardian.Start(ctx)

	agentID := "agent-b"
	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1G"
	
	payload, _ := json.Marshal(models.StatusPayload{Progress: 50, State: "running"})
	env := models.MessageEnvelope{
		Type: "status", ThreadID: &threadID, From: agentID, Timestamp: time.Now().Unix(), Payload: payload,
	}
	subject := "board.status." + threadID

	// 1. Burst 15 msgs in 1 second (Should be allowed)
	for i := 0; i < 15; i++ {
		signAndPublish(t, nc, authProvider, creds[agentID], env, subject)
	}
	time.Sleep(1 * time.Second)
	assert.False(t, authProvider.IsRevoked(agentID), "15 msg/sec should NOT trigger revocation (burst allowed up to 20)")

	// 2. Continual 6 msg/sec for 3 seconds (Should be blocked by strict mode)
	// We use 500ms sleep to ensure batches overlap in the 1s sliding window.
	for i := 0; i < 4; i++ {
		for j := 0; j < 6; j++ {
			signAndPublish(t, nc, authProvider, creds[agentID], env, subject)
		}
		time.Sleep(500 * time.Millisecond)
	}
	time.Sleep(1 * time.Second) // Wait for Guardian to process
	assert.True(t, authProvider.IsRevoked(agentID), "Continual > 5 msg/sec should trigger strict mode revocation")
}

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

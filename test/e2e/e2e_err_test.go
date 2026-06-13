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

func TestE2E_ERR_007_InfiniteLoopDetection(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// サーバー側のコンポーネントを手動で起動
	archiver := &worker.Archiver{
		DB:   db,
		JS:   nc.JS,
		Auth: authProvider,
	}
	go archiver.Start(ctx)

	guardian := &worker.Guardian{
		AuthProvider: authProvider,
		NC:           nc.NC,
		DB:           db,
	}
	go guardian.Start(ctx)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1E"

	// 1. Threadを登録
	_, err := db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)
	require.NoError(t, err)

	time.Sleep(1 * time.Second) // Guardianの起動待ち

	// 2. 循環依存のシミュレーション: a -> b -> c -> a を3回繰り返して閾値を超えるようにする
	publishAssign := func(from string, to string) {
		payload, _ := json.Marshal(models.TaskPayload{Command: "Do work"})
		env := models.MessageEnvelope{
			Type: "assign", ThreadID: &threadID, From: from, To: []string{to}, Timestamp: time.Now().Unix(), Payload: payload,
		}
		signAndPublish(t, nc, authProvider, creds[from], env, "board.assign."+threadID)
		time.Sleep(100 * time.Millisecond) // 順序を保証するための小休止
	}

	for i := 0; i < 3; i++ {
		publishAssign("agent-a", "agent-b")
		publishAssign("agent-b", "agent-c")
		publishAssign("agent-c", "agent-a")
	}

	// ループ検知・DB更新処理を待機
	time.Sleep(3 * time.Second)

	// 3. スレッドのステータスが 'error' になっているか検証
	var status string
	err = db.QueryRow(ctx, "SELECT status FROM threads WHERE id = $1", threadID).Scan(&status)
	require.NoError(t, err)

	assert.Equal(t, "error", status, "循環依存を検知してスレッドステータスが 'error' になること")
}

func TestE2E_ERR_002_CorruptedJSON(t *testing.T) {
	db, nc, authProvider, _, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	// 破損したJSONを送信
	err := nc.NC.Publish("board.task.any", []byte(`{"type": "task", "thread_id": "invalid-json", ...}`))
	require.NoError(t, err)

	time.Sleep(2 * time.Second)
	// クラッシュしていないこと、およびDBに何も増えていないことを確認
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count)
	assert.Equal(t, 0, count, "Corrupted JSON should NOT be persisted")
}

func TestE2E_ERR_003_InvalidThreadID(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	// 存在しない thread_id で結果を送信
	threadID := "NON_EXISTENT_THREAD_ID"
	payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
	env := models.MessageEnvelope{
		Type: "result", ThreadID: &threadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: payload,
	}

	signAndPublish(t, nc, authProvider, creds["agent-b"], env, "board.result."+threadID)

	time.Sleep(2 * time.Second)
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1", threadID).Scan(&count)
	assert.Equal(t, 0, count, "Messages for non-existent threads should be rejected")
}

func TestE2E_ERR_004_Idempotency(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1F"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'open')", threadID)

	payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
	env := models.MessageEnvelope{
		Type: "result", ThreadID: &threadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: payload,
	}

	// 同じ結果を2回送信
	signAndPublish(t, nc, authProvider, creds["agent-b"], env, "board.result."+threadID)
	time.Sleep(500 * time.Millisecond)
	signAndPublish(t, nc, authProvider, creds["agent-b"], env, "board.result."+threadID)

	time.Sleep(2 * time.Second)
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'result'", threadID).Scan(&count)
	assert.Equal(t, 1, count, "Duplicate results should be ignored (Idempotency)")
}

func TestE2E_ERR_006_LateMessage(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1H"
	// スレッドが既に 'done' または 'error' の状態
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'done')", threadID)

	payload, _ := json.Marshal(models.ResultPayload{OutputDir: "out/", ExitCode: 0})
	env := models.MessageEnvelope{
		Type: "result", ThreadID: &threadID, From: "agent-b", Timestamp: time.Now().Unix(), Payload: payload,
	}

	signAndPublish(t, nc, authProvider, creds["agent-b"], env, "board.result."+threadID)

	time.Sleep(2 * time.Second)
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'result'", threadID).Scan(&count)
	assert.Equal(t, 0, count, "Messages for completed/error threads should be discarded")
}

func TestE2E_ERR_005_DuplicateAssignment(t *testing.T) {
	db, nc, authProvider, creds, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	archiver := &worker.Archiver{DB: db, JS: nc.JS, Auth: authProvider}
	go archiver.Start(ctx)
	time.Sleep(1 * time.Second)

	threadID := "01HGWY5X9A7Z4K2M3Q8P6R0V1I"
	db.Exec(ctx, "INSERT INTO threads (id, created_by_agent, status) VALUES ($1, 'agent-a', 'assigned')", threadID)
	db.Exec(ctx, "UPDATE threads SET assigned_agent = 'agent-b' WHERE id = $1", threadID)

	// Agent C への重複アサイン試行
	payload, _ := json.Marshal(models.AssignPayload{Reason: "Force assign"})
	env := models.MessageEnvelope{
		Type: "assign", ThreadID: &threadID, From: "agent-a", To: []string{"agent-c"}, Timestamp: time.Now().Unix(), Payload: payload,
	}

	signAndPublish(t, nc, authProvider, creds["agent-a"], env, "board.assign."+threadID)

	time.Sleep(2 * time.Second)
	var count int
	db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE thread_id = $1 AND type = 'assign' AND to_agents @> ARRAY['agent-c']", threadID).Scan(&count)
	assert.Equal(t, 0, count, "Duplicate assignments for already assigned threads should be rejected")
}

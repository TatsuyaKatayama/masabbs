package e2e

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/TatsuyaKatayama/masabbs/internal/models"
	"github.com/TatsuyaKatayama/masabbs/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_ERR_007_InfiniteLoopDetection(t *testing.T) {
	db, nc, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// サーバー側のコンポーネントを手動で起動
	authProvider, _ := auth.NewProvider()
	archiver := &worker.Archiver{
		DB: db,
		JS: nc.JS,
		Auth: authProvider,
	}
	go archiver.Start(ctx)

	guardian := &worker.Guardian{
		AuthProvider: authProvider,
		NC:           nc.NC,
		DB:           db, // GuardianにDBアクセスを追加してstatus変更できるようにする想定
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
		data, _ := json.Marshal(env)
		nc.NC.Publish("board.assign."+threadID, data)
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

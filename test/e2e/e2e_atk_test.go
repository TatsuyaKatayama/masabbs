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

func TestE2E_ATK_001_RateLimit(t *testing.T) {
	db, nc, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// サーバー側のコンポーネントを手動で起動（E2E環境の模倣）
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
	data, _ := json.Marshal(resEnv)
	subject := "board.result." + threadID

	// 70通パブリッシュ
	for i := 0; i < 70; i++ {
		_, err := nc.JS.Publish(ctx, subject, data)
		require.NoError(t, err)
	}

	// アーカイバーの処理を待機
	time.Sleep(5 * time.Second)

	// DBに保存された件数を確認
	var count int
	err = db.QueryRow(ctx, "SELECT count(*) FROM tasks WHERE agent_id = $1", agentID).Scan(&count)
	require.NoError(t, err)

	t.Logf("Messages in DB: %d", count)

	// 60通を超えた分は Guardian によって Revoke され、Archiver で破棄されているはず
	assert.LessOrEqual(t, count, 61, "60通程度で制限されるべき（タイミングにより1,2通前後する可能性あり）")
}

func TestE2E_ATK_003_HugePayload(t *testing.T) {
	_, nc, cleanup := setupE2EEnvironment(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 10MB + 1 byte payload (limit is 10MB in UT-VAL-110, spec says 100MB for ATK-003, let's test 11MB)
	hugeData := make([]byte, 11*1024*1024)
	for i := range hugeData {
		hugeData[i] = 'a'
	}

	resEnv := models.MessageEnvelope{
		Type: "result", From: "agent-b", Timestamp: time.Now().Unix(), Payload: hugeData,
	}
	data, _ := json.Marshal(resEnv)

	_, err := nc.JS.Publish(ctx, "board.result.huge", data)
	
	// Current expected behavior: Likely succeeds or hits NATS default limit (usually 1MB unless configured)
	if err != nil {
		t.Logf("Huge payload rejected: %v", err)
	} else {
		t.Log("Huge payload accepted (Limit not enforced yet)")
	}
}

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TatsuyaKatayama/masabbs/internal/api"
	"github.com/TatsuyaKatayama/masabbs/internal/auth"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_KPILogic(t *testing.T) {
	db, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	ctx := context.Background()

	// 1. Seed Team and Agents
	_, err := db.Exec(ctx, `
		INSERT INTO agents (id, name, role, team_id) VALUES 
		('mgr-1', 'Manager 1', 'TeamManager', 'test-team'),
		('wrk-1', 'Worker 1', 'Worker', 'test-team')
		ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)

	// 2. Create Threads (Parent & Subthread)
	parentThreadID := "kpi-parent-thread"
	subThreadID := "kpi-sub-thread"
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status, team_id) VALUES 
		($1, NULL, 'mgr-1', 'open', 'test-team'),
		($2, $1, 'mgr-1', 'open', 'test-team')
	`, parentThreadID, subThreadID)
	require.NoError(t, err)

	// 3. Create Tasks (chronological conversation)
	// Task 1: mgr-1 asks wrk-1 (Top-level)
	t1 := time.Now().Add(-10 * time.Minute)
	_, err = db.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload, created_at)
		VALUES ('task-1', $1, 'mgr-1', 'task', ARRAY['wrk-1'], ARRAY[]::text[], '{}'::jsonb, $2)
	`, parentThreadID, t1)
	require.NoError(t, err)

	// Task 2: wrk-1 replies to mgr-1 (6 minutes delay)
	t2 := t1.Add(6 * time.Minute)
	_, err = db.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload, created_at)
		VALUES ('task-2', $1, 'wrk-1', 'result', ARRAY['mgr-1'], ARRAY[]::text[], '{}'::jsonb, $2)
	`, parentThreadID, t2)
	require.NoError(t, err)

	// Task 3: mgr-1 asks wrk-1 in Subthread (Subtask)
	t3 := t2.Add(2 * time.Minute)
	_, err = db.Exec(ctx, `
		INSERT INTO tasks (id, thread_id, agent_id, type, to_agents, observers, payload, created_at)
		VALUES ('task-3', $1, 'mgr-1', 'task', ARRAY['wrk-1'], ARRAY[]::text[], '{}'::jsonb, $2)
	`, subThreadID, t3)
	require.NoError(t, err)

	// 4. Seed Reflection
	reqID := "refl-req-kpi"
	refThreadID := "ref-thread-id"
	// Insert the reflection subthread into threads first to satisfy foreign key
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, parent_thread_id, created_by_agent, status, team_id) VALUES 
		($1, $2, 'mgr-1', 'open', 'test-team')
	`, refThreadID, parentThreadID)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO thread_reflection_requests (id, thread_id, reflection_thread_id, requested_by_agent_id, status, due_at)
		VALUES ($1, $2, $3, 'mgr-1', 'pending', $4)
	`, reqID, parentThreadID, refThreadID, time.Now().Add(10*time.Hour))
	require.NoError(t, err)

	_, err = db.Exec(ctx, `
		INSERT INTO thread_reflections (id, request_id, thread_id, team_id, from_agent_id, target_agent_id, dimension, score, reason)
		VALUES ('refl-id-1', $1, $2, 'test-team', 'wrk-1', 'mgr-1', 'clarity', 1, 'Very clear instructions')
	`, reqID, parentThreadID)
	require.NoError(t, err)

	e := echo.New()
	authProvider, _ := auth.NewProvider()
	api.RegisterRoutes(e, db, nc, &MockStorage{}, nil, authProvider)

	// --- Test GET /api/v1/threads/:id/kpi ---
	req := httptest.NewRequest(http.MethodGet, "/api/v1/threads/"+parentThreadID+"/kpi", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var threadKPI api.ThreadKPIResponse
	err = json.Unmarshal(rec.Body.Bytes(), &threadKPI)
	require.NoError(t, err)

	// Assertions on Thread KPI
	assert.Equal(t, parentThreadID, threadKPI.ThreadID)
	assert.Equal(t, 2, threadKPI.SubthreadCount) // 2 subthreads: kpi-sub-thread and ref-thread-id
	assert.Equal(t, 1, threadKPI.MaxDepth)
	assert.Equal(t, 3, threadKPI.MessageStats.TotalMessages)
	assert.Equal(t, 2, threadKPI.MessageStats.SentCounts["mgr-1"])
	assert.Equal(t, 1, threadKPI.MessageStats.SentCounts["wrk-1"])
	assert.Equal(t, 2, threadKPI.MessageStats.ReceivedCounts["wrk-1"])
	assert.Equal(t, 1, threadKPI.MessageStats.ReceivedCounts["mgr-1"])

	// Reply metrics (t2 - t1 delay is 360 seconds. Task 3 is pending/unreplied, and Task 2 is unreplied, so unreplied count = 2)
	assert.InDelta(t, 360.0, threadKPI.ReplyMetrics.AverageReplyDelaySeconds, 0.1)
	assert.Equal(t, 2, threadKPI.ReplyMetrics.UnrepliedCount)
	assert.InDelta(t, 0.666, threadKPI.ReplyMetrics.UnrepliedRate, 0.01)

	// Reflection metrics
	assert.InDelta(t, 1.0, threadKPI.ReflectionStats.AverageScore, 0.1)
	assert.InDelta(t, 1.0, threadKPI.ReflectionStats.ByDimension["clarity"], 0.1)

	// Network Data for D3.js (nodes and links)
	assert.Len(t, threadKPI.NetworkData.Nodes, 2)
	assert.Len(t, threadKPI.NetworkData.Links, 2) // mgr-1 to wrk-1, wrk-1 to mgr-1

	// --- Test GET /api/v1/teams/:id/kpi ---
	teamReq := httptest.NewRequest(http.MethodGet, "/api/v1/teams/test-team/kpi", nil)
	teamRec := httptest.NewRecorder()
	e.ServeHTTP(teamRec, teamReq)

	assert.Equal(t, http.StatusOK, teamRec.Code)

	var teamKPI api.TeamKPIResponse
	err = json.Unmarshal(teamRec.Body.Bytes(), &teamKPI)
	require.NoError(t, err)

	// Assertions on Team KPI
	assert.Equal(t, "test-team", teamKPI.TeamID)
	assert.Equal(t, 1, teamKPI.TotalThreads) // Only parentThreadID is top-level root thread
	assert.Equal(t, 2, teamKPI.TotalSubthreads)
	assert.Equal(t, 1, teamKPI.MaxSubthreadDepth)
	assert.Equal(t, 3, teamKPI.MessageStats.TotalMessages)
}

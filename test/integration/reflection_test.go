package integration

import (
	"bytes"
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

type MockStorage struct{}

func (m *MockStorage) GetThreadInputPath(threadID string) string {
	return "tasks/" + threadID + "/input/"
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

func TestIntegration_ReflectionFlow(t *testing.T) {
	db, nc, cleanup := setupIntegrationEnvironment(t)
	defer cleanup()

	// Seed multiple team agents (bosses, colleagues, subordinates)
	ctx := context.Background()
	_, err := db.Exec(ctx, `
		INSERT INTO agents (id, name, role, team_id) VALUES 
		('manager-1', 'Manager 1', 'TeamManager', 'test-team'),
		('chef-1', 'Chef 1', 'Chef', 'test-team'),
		('worker-2', 'Worker 2', 'Worker', 'test-team')
		ON CONFLICT DO NOTHING
	`)
	require.NoError(t, err)

	// Create parent thread
	parentThreadID := "thread-reflection-parent"
	_, err = db.Exec(ctx, `
		INSERT INTO threads (id, created_by_agent, status, team_id)
		VALUES ($1, 'manager-1', 'open', 'test-team')
	`, parentThreadID)
	require.NoError(t, err)

	e := echo.New()
	authProvider, _ := auth.NewProvider()
	hub := api.NewHub(nc.NC, db)
	api.RegisterRoutes(e, db, nc, &MockStorage{}, hub, authProvider)

	// --- 1. Request Reflection ---
	reqBody := api.RequestReflectionRequest{
		RequestedByAgent: "manager-1",
	}
	bodyBytes, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/threads/"+parentThreadID+"/reflection-requests", bytes.NewReader(bodyBytes))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp api.RequestReflectionResponse
	err = json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.RequestID)
	assert.NotEmpty(t, resp.ReflectionThreadID)

	// Verify DB state
	var reqCount int
	err = db.QueryRow(ctx, "SELECT count(*) FROM thread_reflection_requests WHERE id = $1", resp.RequestID).Scan(&reqCount)
	require.NoError(t, err)
	assert.Equal(t, 1, reqCount)

	// Verify subthread created and inherited team-id
	var subThreadTeamID string
	err = db.QueryRow(ctx, "SELECT team_id FROM threads WHERE id = $1", resp.ReflectionThreadID).Scan(&subThreadTeamID)
	require.NoError(t, err)
	assert.Equal(t, "test-team", subThreadTeamID)

	// --- 2. Submit Reflection (Succeeds) ---
	subBody := api.SubmitReflectionRequest{
		RequestID:     resp.RequestID,
		FromAgentID:   "worker-2",
		TargetAgentID: "chef-1", // Colleague/boss in same team
		Dimension:     "clarity",
		Score:         1,
		Reason:        "Great instruction",
	}
	subBytes, _ := json.Marshal(subBody)
	subReq := httptest.NewRequest(http.MethodPost, "/api/v1/reflections", bytes.NewReader(subBytes))
	subReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	subRec := httptest.NewRecorder()
	e.ServeHTTP(subRec, subReq)

	assert.Equal(t, http.StatusCreated, subRec.Code)

	// Verify DB state for reflections
	var reflectionCount int
	err = db.QueryRow(ctx, "SELECT count(*) FROM thread_reflections WHERE request_id = $1", resp.RequestID).Scan(&reflectionCount)
	require.NoError(t, err)
	assert.Equal(t, 1, reflectionCount)

	// --- 3. Submit Reflection to unrelated agent (Fails) ---
	// Seed an agent on another team
	_, err = db.Exec(ctx, "INSERT INTO teams (id, name) VALUES ('other-team', 'Other Team')")
	require.NoError(t, err)
	_, err = db.Exec(ctx, "INSERT INTO agents (id, name, role, team_id) VALUES ('worker-other', 'Worker Other', 'Worker', 'other-team')")
	require.NoError(t, err)

	unrelatedBody := api.SubmitReflectionRequest{
		RequestID:     resp.RequestID,
		FromAgentID:   "worker-2",
		TargetAgentID: "worker-other", // Unrelated agent on another team
		Dimension:     "clarity",
		Score:         1,
		Reason:        "Unrelated",
	}
	unrelatedBytes, _ := json.Marshal(unrelatedBody)
	unrelatedReq := httptest.NewRequest(http.MethodPost, "/api/v1/reflections", bytes.NewReader(unrelatedBytes))
	unrelatedReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	unrelatedRec := httptest.NewRecorder()
	e.ServeHTTP(unrelatedRec, unrelatedReq)

	assert.Equal(t, http.StatusBadRequest, unrelatedRec.Code)
	assert.Contains(t, unrelatedRec.Body.String(), "INVALID_TARGET_AGENT")

	// --- 4. Submit Reflection on expired request (Fails) ---
	// Update due_at to the past
	_, err = db.Exec(ctx, "UPDATE thread_reflection_requests SET due_at = $1 WHERE id = $2", time.Now().Add(-1*time.Hour), resp.RequestID)
	require.NoError(t, err)

	expiredBody := api.SubmitReflectionRequest{
		RequestID:     resp.RequestID,
		FromAgentID:   "worker-2",
		TargetAgentID: "chef-1",
		Dimension:     "clarity",
		Score:         1,
		Reason:        "Expired",
	}
	expiredBytes, _ := json.Marshal(expiredBody)
	expiredReq := httptest.NewRequest(http.MethodPost, "/api/v1/reflections", bytes.NewReader(expiredBytes))
	expiredReq.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	expiredRec := httptest.NewRecorder()
	e.ServeHTTP(expiredRec, expiredReq)

	assert.Equal(t, http.StatusBadRequest, expiredRec.Code)
	assert.Contains(t, expiredRec.Body.String(), "REFLECTION_REQUEST_EXPIRED")
}

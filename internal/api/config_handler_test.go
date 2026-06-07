// config_handler_test.go
package api

import (
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"

    "github.com/TatsuyaKatayama/masabbs/internal/models"
    "github.com/labstack/echo/v4"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)


func TestConfigHandlers(t *testing.T) {
    db, cleanup := setupDB(t)
    defer cleanup()

    ctx := context.Background()
    // Insert sample data
    _, err := db.Exec(ctx, `INSERT INTO teams (id, name, description, mission) VALUES ('team-1','Team One','desc','mission')`)
    require.NoError(t, err)
    _, err = db.Exec(ctx, `INSERT INTO agents (id, name, role, mission, tools, capabilities, status, team_id, ui_pos_x, ui_pos_y) VALUES ('agent-1','Agent One','worker','mission','[]','[]','offline','team-1',0,0)`)
    require.NoError(t, err)

    // Prepare Echo and handler
    e := echo.New()
    h := &Handler{DB: db}

    // Register routes used in this test
    e.POST("/api/v1/configs", h.CreateConfig)
    e.GET("/api/v1/configs", h.GetConfigs)
    e.POST("/api/v1/configs/:id/load", h.LoadConfig)
    e.DELETE("/api/v1/configs/:id", h.DeleteConfig)

    // 1. Create a new config snapshot
    payload := `{"name":"snapshot-1","description":"first snapshot"}`
    req := httptest.NewRequest(http.MethodPost, "/api/v1/configs", strings.NewReader(payload))
    req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
    rec := httptest.NewRecorder()
    e.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusCreated, rec.Code)

    var created models.Config
    err = json.Unmarshal(rec.Body.Bytes(), &created)
    require.NoError(t, err)
    assert.NotEmpty(t, created.ID)
    assert.Equal(t, "snapshot-1", created.Name)

    // 2. List configs and ensure the created one appears
    req = httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
    rec = httptest.NewRecorder()
    e.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusOK, rec.Code)
    var list []models.Config
    err = json.Unmarshal(rec.Body.Bytes(), &list)
    require.NoError(t, err)
    assert.Len(t, list, 1)
    assert.Equal(t, created.ID, list[0].ID)

    // 3. Load the configuration
    loadPath := "/api/v1/configs/" + created.ID + "/load"
    req = httptest.NewRequest(http.MethodPost, loadPath, nil)
    rec = httptest.NewRecorder()
    e.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusOK, rec.Code)
    var loadResp map[string]string
    err = json.Unmarshal(rec.Body.Bytes(), &loadResp)
    require.NoError(t, err)
    assert.Equal(t, "configuration loaded successfully", loadResp["message"])

    // 4. Delete the configuration
    delPath := "/api/v1/configs/" + created.ID
    req = httptest.NewRequest(http.MethodDelete, delPath, nil)
    rec = httptest.NewRecorder()
    e.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusNoContent, rec.Code)

    // 5. Verify it is removed from list
    req = httptest.NewRequest(http.MethodGet, "/api/v1/configs", nil)
    rec = httptest.NewRecorder()
    e.ServeHTTP(rec, req)
    assert.Equal(t, http.StatusOK, rec.Code)
    err = json.Unmarshal(rec.Body.Bytes(), &list)
    require.NoError(t, err)
    assert.Len(t, list, 0)
}

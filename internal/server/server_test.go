package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupServerTest(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../migrations")
	require.NoError(t, err)

	cfg := &config.Config{LLMModel: "mock", LLMAPIKey: "test"}
	srv := New(s, cfg, zap.NewNop())
	return srv, s
}

func TestServer_Health(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

func TestServer_CreateSession_MissingGoal(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(`{"mode":"run"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListSessions_Empty(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(0), body["count"])
}

func TestServer_GetSession_NotFound(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetReport_NotFound(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent/report", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_CancelSession_NotRunning(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent/cancel", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetSession_AfterCreate(t *testing.T) {
	srv, s := setupServerTest(t)

	// Create a session directly in the store.
	sess, err := s.CreateSession(context.Background(), "run", "test goal", "project")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, sess.ID, body["id"])
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, "test goal", body["goal"])
}

func TestServer_ListSessions_WithData(t *testing.T) {
	srv, s := setupServerTest(t)

	_, err := s.CreateSession(context.Background(), "run", "goal 1", "proj")
	require.NoError(t, err)
	_, err = s.CreateSession(context.Background(), "verify", "goal 2", "proj")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(2), body["count"])
}

func TestServer_GetReport_WithVerdicts(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "report test", "proj")
	require.NoError(t, err)

	// Create trace + verdict.
	traceID, err := s.CreateTrace(ctx, sess.ID, "agent", "/api/test")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "/api/test", "pass", 0.95, "judge", "looks good", nil)
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/report", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))

	sessionData, ok := body["session"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, sess.ID, sessionData["id"])

	verdicts, ok := body["verdicts"].([]any)
	require.True(t, ok)
	assert.Len(t, verdicts, 1)
}

func TestServer_GetReport_PlainText(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "plain report", "proj")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/report", nil)
	req.Header.Set("Accept", "text/plain")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Session: "+sess.ID)
	assert.Contains(t, body, "Goal: plain report")
	assert.Contains(t, body, "Status: running")
}

func TestServer_ListSessions_LimitParam(t *testing.T) {
	srv, s := setupServerTest(t)

	for i := 0; i < 5; i++ {
		_, err := s.CreateSession(context.Background(), "run", "goal", "proj")
		require.NoError(t, err)
	}

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=2", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(2), body["count"])
}

func TestServer_CancelSession_Running(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "cancel me", "proj")
	require.NoError(t, err)

	// Manually register a cancel function to simulate a running session.
	srv.mu.Lock()
	ctx2, cancel := context.WithCancel(ctx)
	srv.runs[sess.ID] = cancel
	srv.mu.Unlock()
	defer cancel()
	_ = ctx2

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "aborted", body["status"])

	// Verify session status in DB.
	updated, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "aborted", updated.Status)
}

func TestServer_CreateSession_InvalidJSON(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

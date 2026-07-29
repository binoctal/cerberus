package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
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

// setupServerWithMock injects a MockClient via clientFactory to avoid real LLM calls.
func setupServerWithMock(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	srv, s := setupServerTest(t)
	srv.clientFactory = func(cfg llm.ClientConfig) (llm.Client, error) {
		return llm.NewMockClient(map[string]string{
			"default": `{"plan":[]}`,
		}), nil
	}
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
	assert.NotEmpty(t, body["time"])
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

func TestServer_CreateSession_Success(t *testing.T) {
	srv, s := setupServerWithMock(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(`{"mode":"run","goal":"test the API"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.NotEmpty(t, body["id"])
	assert.Equal(t, "running", body["status"])
	assert.Equal(t, "run", body["mode"])

	// Verify session was persisted.
	sess, err := s.GetSession(context.Background(), body["id"])
	require.NoError(t, err)
	assert.Equal(t, "test the API", sess.Goal)
	assert.Equal(t, "run", sess.Mode)

	// Verify the cancel function is registered for the running session.
	srv.mu.Lock()
	_, hasCancel := srv.runs[body["id"]]
	srv.mu.Unlock()
	assert.True(t, hasCancel, "session should have a cancel function registered")

	// Cancel to clean up the background goroutine.
	cancelReq := httptest.NewRequest(http.MethodPost,
		"/api/v1/sessions/"+body["id"]+"/cancel", nil)
	cancelW := httptest.NewRecorder()
	handler.ServeHTTP(cancelW, cancelReq)
	assert.Equal(t, http.StatusOK, cancelW.Code)
}

func TestServer_CreateSession_DefaultMode(t *testing.T) {
	srv, _ := setupServerWithMock(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(`{"goal":"test default mode"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, "run", body["mode"])
}

func TestServer_CreateSession_EmptyBody(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Empty body with no goal should return bad request.
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

func TestServer_ListSessions_InvalidLimit(t *testing.T) {
	srv, s := setupServerTest(t)

	for i := 0; i < 25; i++ {
		_, err := s.CreateSession(context.Background(), "run", "goal", "proj")
		require.NoError(t, err)
	}

	handler := srv.Handler()

	// Negative limit should use default (20).
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=-1", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(20), body["count"])

	// Limit exceeding max (100) should use default (20).
	req = httptest.NewRequest(http.MethodGet, "/api/v1/sessions?limit=200", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, float64(20), body["count"])
}

func TestServer_GetSession_NotFound(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Contains(t, body["error"], "not found")
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

func TestServer_GetReport_NotFound(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent/report", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_GetReport_WithVerdicts(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "report test", "proj")
	require.NoError(t, err)

	// Create trace + verdict.
	traceID, err := s.CreateTrace(ctx, sess.ID, "agent", "/api/test")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "/api/test", "pass", 0.95, "judge", "looks good", nil, store.FailureReasonNone, false)
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
	assert.Equal(t, "text/plain; charset=utf-8", w.Header().Get("Content-Type"))
	body := w.Body.String()
	assert.Contains(t, body, "Session: "+sess.ID)
	assert.Contains(t, body, "Goal: plain report")
	assert.Contains(t, body, "Status: running")
}

func TestServer_GetReport_Markdown(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "md report", "proj")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/report", nil)
	req.Header.Set("Accept", "text/markdown")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/markdown; charset=utf-8", w.Header().Get("Content-Type"))
	body := w.Body.String()
	assert.Contains(t, body, "# Cerberus Test Report")
	assert.Contains(t, body, sess.ID)
}

func TestServer_GetReport_HTML(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "html report", "proj")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/"+sess.ID+"/report", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	body := w.Body.String()
	assert.Contains(t, body, "Cerberus Test Report")
	assert.Contains(t, body, sess.ID)
}

func TestServer_CancelSession_NotRunning(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/nonexistent/cancel", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
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

func TestServer_CancelSession_AlreadyCompleted(t *testing.T) {
	srv, s := setupServerTest(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "already done", "proj")
	require.NoError(t, err)
	// Mark as completed (no cancel func in srv.runs).
	err = s.UpdateSessionStatus(ctx, sess.ID, "completed")
	require.NoError(t, err)

	handler := srv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+sess.ID+"/cancel", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Session is not in the active runs map, so cancel returns 404.
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestServer_DashboardRedirect(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "/dashboard/")
}

func TestServer_DashboardIndex(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/dashboard/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Cerberus")
}

func TestServer_CreateSession_WithURL(t *testing.T) {
	srv, _ := setupServerWithMock(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(`{"goal":"test","url":"http://custom:8080"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.NotEmpty(t, body["id"])
}

func TestServer_Health_TimeFormat(t *testing.T) {
	srv, _ := setupServerTest(t)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	// Verify time is valid RFC3339.
	_, err := time.Parse(time.RFC3339, body["time"])
	assert.NoError(t, err, "health endpoint time should be RFC3339")
}

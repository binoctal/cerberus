package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHTTPAction_APIRequest_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/users", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"users":[]}`))
	}))
	defer server.Close()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionAPIRequest,
		Target: server.URL + "/api/v1/users",
		Method: "GET",
	})

	assert.True(t, obs.Success)
	assert.Equal(t, 200, obs.StatusCode)
	assert.Contains(t, obs.Body, "users")
}

func TestHTTPAction_APIRequest_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionAPIRequest,
		Target: server.URL + "/api/v1/users",
		Method: "POST",
		Value:  `{"name":"test"}`,
	})

	assert.True(t, obs.Success)
	assert.Equal(t, 201, obs.StatusCode)
}

func TestHTTPAction_Navigate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html>Dashboard</html>"))
	}))
	defer server.Close()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionNavigate,
		Target: server.URL + "/dashboard",
	})

	assert.True(t, obs.Success)
	assert.Equal(t, 200, obs.StatusCode)
}

func TestHTTPAction_Wait(t *testing.T) {
	exec := NewHTTPActionExecutor("", zap.NewNop())
	start := time.Now()
	obs := exec.Execute(context.Background(), Action{
		Type:  ActionWait,
		Value: "100ms",
	})
	elapsed := time.Since(start)

	assert.True(t, obs.Success)
	assert.True(t, elapsed >= 90*time.Millisecond, "should wait ~100ms, waited %s", elapsed)
}

func TestHTTPAction_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(ctx, Action{
		Type:   ActionAPIRequest,
		Target: server.URL + "/slow",
		Method: "GET",
	})

	assert.False(t, obs.Success)
	assert.Contains(t, obs.Error, "http request")
}

func TestHTTPAction_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionAPIRequest,
		Target: server.URL + "/missing",
		Method: "GET",
	})

	assert.False(t, obs.Success)
	assert.Equal(t, 404, obs.StatusCode)
}

func TestHTTPAction_UnsupportedClick(t *testing.T) {
	exec := NewHTTPActionExecutor("", zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionClick,
		Target: "#submit",
	})

	assert.False(t, obs.Success)
	assert.Contains(t, obs.Error, "not supported")
}

func TestHTTPAction_RelativeTarget(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPActionExecutor(server.URL, zap.NewNop())
	obs := exec.Execute(context.Background(), Action{
		Type:   ActionNavigate,
		Target: "/api/health",
	})

	assert.True(t, obs.Success)
}

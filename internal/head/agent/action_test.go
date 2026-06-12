package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestHTTPExecutor_APIRequest_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/users", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"users":[]}`))
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/api/v1/users",
	})

	httpRes := result.(types.HTTPResult)
	assert.True(t, httpRes.OK)
	assert.Equal(t, 200, httpRes.StatusCode)
	assert.Contains(t, httpRes.Body, "users")
}

func TestHTTPExecutor_APIRequest_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "POST",
		URL:    server.URL + "/api/v1/users",
		Body:   `{"name":"test"}`,
	})

	httpRes := result.(types.HTTPResult)
	assert.True(t, httpRes.OK)
	assert.Equal(t, 201, httpRes.StatusCode)
}

func TestHTTPExecutor_Navigate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html>Dashboard</html>"))
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.NavigateAction{
		URL: server.URL + "/dashboard",
	})

	httpRes := result.(types.HTTPResult)
	assert.True(t, httpRes.OK)
	assert.Equal(t, 200, httpRes.StatusCode)
}

func TestHTTPExecutor_Wait(t *testing.T) {
	exec := NewWaitExecutor()
	start := time.Now()
	result := exec.Execute(context.Background(), types.WaitAction{Duration: "100ms"})
	elapsed := time.Since(start)

	waitRes := result.(types.WaitResult)
	assert.True(t, waitRes.OK)
	assert.True(t, elapsed >= 90*time.Millisecond, "should wait ~100ms, waited %s", elapsed)
}

func TestHTTPExecutor_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(ctx, types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/slow",
	})

	httpRes := result.(types.HTTPResult)
	assert.False(t, httpRes.OK)
	assert.Contains(t, httpRes.Err, "context deadline exceeded")
}

func TestHTTPExecutor_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/missing",
	})

	httpRes := result.(types.HTTPResult)
	assert.False(t, httpRes.OK)
	assert.Equal(t, 404, httpRes.StatusCode)
}

func TestHTTPExecutor_UnsupportedAction(t *testing.T) {
	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.WaitAction{Duration: "1s"})

	errRes := result.(types.ErrorResult)
	assert.Contains(t, errRes.Err, "unsupported action")
}

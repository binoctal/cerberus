package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/binoctal/cerberus/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHTTPExecutor_GET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/health", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/api/health",
	})

	hr := result.(types.HTTPResult)
	assert.True(t, hr.OK)
	assert.Equal(t, http.StatusOK, hr.StatusCode)
	assert.Contains(t, hr.Body, "ok")
	assert.Empty(t, hr.Err)
}

func TestHTTPExecutor_POST(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "POST",
		URL:    server.URL + "/api/items",
		Body:   `{"name":"test"}`,
	})

	hr := result.(types.HTTPResult)
	assert.True(t, hr.OK)
	assert.Equal(t, http.StatusCreated, hr.StatusCode)
}

func TestHTTPExecutor_PUT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "PUT",
		URL:    server.URL + "/api/items/1",
		Body:   `{"name":"updated"}`,
	})

	hr := result.(types.HTTPResult)
	assert.True(t, hr.OK)
}

func TestHTTPExecutor_DELETE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "DELETE",
		URL:    server.URL + "/api/items/1",
	})

	hr := result.(types.HTTPResult)
	assert.True(t, hr.OK)
	assert.Equal(t, http.StatusNoContent, hr.StatusCode)
}

func TestHTTPExecutor_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/fail",
	})

	hr := result.(types.HTTPResult)
	assert.False(t, hr.OK)
	assert.Equal(t, http.StatusInternalServerError, hr.StatusCode)
}
func TestHTTPExecutor_CancelledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response.
		select {}
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(ctx, types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/slow",
	})

	hr := result.(types.HTTPResult)
	assert.False(t, hr.OK)
	assert.NotEmpty(t, hr.Err)
}

func TestHTTPExecutor_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET",
		URL:    server.URL + "/api/secure",
		Headers: map[string]string{
			"Authorization": "Bearer token123",
			"X-Custom":      "custom-value",
		},
	})

	hr := result.(types.HTTPResult)
	require.True(t, hr.OK)
}

func TestHTTPExecutor_NavigateAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.NavigateAction{
		URL: server.URL + "/page",
	})

	hr := result.(types.HTTPResult)
	assert.True(t, hr.OK)
}

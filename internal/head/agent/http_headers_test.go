package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	return u.Host
}

// Service-level headers (matched by request URL host) are injected on every
// request, including Host (set via req.Host for domain-routed gateways).
func TestHTTPExecutor_ServiceHeadersInjected(t *testing.T) {
	var gotHost, gotSvc string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		gotSvc = r.Header.Get("X-Service")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutorWithServiceHeaders(zap.NewNop(), map[string]map[string]string{
		hostOf(t, server.URL): {"Host": "api.opendune.com", "X-Service": "svc"},
	})
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET", URL: server.URL + "/api",
	})

	hr := result.(types.HTTPResult)
	require.True(t, hr.OK)
	assert.Equal(t, "api.opendune.com", gotHost)
	assert.Equal(t, "svc", gotSvc)
}

// Action headers override service headers (same key). Lets a negative test
// mutate Host or Authorization.
func TestHTTPExecutor_ActionHeadersOverrideService(t *testing.T) {
	var gotHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHost = r.Host
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutorWithServiceHeaders(zap.NewNop(), map[string]map[string]string{
		hostOf(t, server.URL): {"Host": "api.opendune.com"},
	})
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method:  "GET",
		URL:     server.URL + "/api",
		Headers: map[string]string{"Host": "wrong.example"},
	})

	hr := result.(types.HTTPResult)
	require.True(t, hr.OK)
	assert.Equal(t, "wrong.example", gotHost)
}

// Action may remove an injected header by setting it to empty string — needed
// for "no Authorization" negative cases when the service/actor provides one.
func TestHTTPExecutor_ActionEmptyRemovesServiceHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutorWithServiceHeaders(zap.NewNop(), map[string]map[string]string{
		hostOf(t, server.URL): {"Authorization": "Bearer default"},
	})
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method:  "GET",
		URL:     server.URL + "/api",
		Headers: map[string]string{"Authorization": ""}, // remove
	})

	hr := result.(types.HTTPResult)
	require.True(t, hr.OK)
	assert.Equal(t, "", gotAuth)
}

// Requests to a host with no service config send no injected headers (and the
// plain NewHTTPExecutor stays backward compatible).
func TestHTTPExecutor_NoServiceHeadersUnchanged(t *testing.T) {
	var gotSvc string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSvc = r.Header.Get("X-Service")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	result := exec.Execute(context.Background(), types.HTTPAction{
		Method: "GET", URL: server.URL + "/api",
	})

	hr := result.(types.HTTPResult)
	require.True(t, hr.OK)
	assert.Equal(t, "", gotSvc)
}

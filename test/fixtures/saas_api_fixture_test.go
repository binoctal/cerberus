package fixtures

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestSaaSAPIFixture(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/users":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[{"id":"1"}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "" // SaaS mode (services has URL)
	cfg.Services = []project.Service{{Name: "api", URL: srv.URL, Health: "/health"}}
	mockClient := llm.NewMockClient(MockResponses("/health"))
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(50000, 5000))
	_ = driver // session uses mockClient directly

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test SaaS fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: ".",
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "SaaS session should complete")

	assert.NotNil(t, sess.Contract, "contract produced in SaaS mode")
}

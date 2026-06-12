package session

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/escalation"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testStoreWithMigrations(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	return s
}

func TestNewSession(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(map[string]string{"default": `{"status":"pass"}`})
	logger := zap.NewNop()

	cfg := project.DefaultConfig()
	cfg.Project.Name = "test-project"

	sess, err := NewSession(context.Background(), ModeRun, "test goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)
	assert.NotEmpty(t, sess.ID)
	assert.Equal(t, ModeRun, sess.Mode)
	assert.Equal(t, "test goal", sess.Goal)
	assert.Equal(t, ".", sess.ProjectDir)
	assert.NotNil(t, sess.Driver)
	assert.NotNil(t, sess.Gate)

	// Verify persisted in store.
	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "running", dbSess.Status)
	assert.Equal(t, "test goal", dbSess.Goal)
	assert.Equal(t, "test-project", dbSess.ProjectName)
}

func TestNewSession_StoreError(t *testing.T) {
	s := testStoreWithMigrations(t)
	_ = s.Close() // Close store to trigger error.

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	_, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create session")
}

func TestNewSession_NilGate(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// nil gate should be replaced with NoOpGate.
	_, ok := sess.Gate.(escalation.NoOpGate)
	assert.True(t, ok, "nil gate should be replaced with NoOpGate")
}

func TestNewSession_WithExplicitGate(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	gate := escalation.NoOpGate{}
	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, gate, ".")
	require.NoError(t, err)
	assert.Equal(t, gate, sess.Gate)
}

func TestSession_ResolveBaseURL(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()

	t.Run("with services", func(t *testing.T) {
		cfg := project.DefaultConfig()
		cfg.Services = []project.Service{
			{Name: "api", URL: "http://localhost:3000"},
		}
		sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:3000", sess.resolveBaseURL())
	})

	t.Run("without services", func(t *testing.T) {
		cfg := project.DefaultConfig()
		sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
		require.NoError(t, err)
		assert.Equal(t, "", sess.resolveBaseURL())
	})
}

func TestSession_Close(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	mockClient := llm.NewMockClient(nil)
	logger := zap.NewNop()
	cfg := project.DefaultConfig()

	sess, err := NewSession(context.Background(), ModeRun, "goal", &cfg, s, mockClient, logger, nil, ".")
	require.NoError(t, err)

	// Close should not panic.
	assert.NotPanics(t, func() {
		sess.Close()
	})
}

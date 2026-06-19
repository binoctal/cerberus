package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/llm"
)

func TestRun_InvokesAutoTestPhase(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	// Use a temporary directory without Go files to avoid running actual tests
	tmpDir := t.TempDir()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "verify service health",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: tmpDir,
	})
	require.NoError(t, err)

	sess.AutoTestSafety = "dry-run"
	err = sess.Run(context.Background())
	require.NoError(t, err, "Run should complete without error")

	// Verify AutoTest phase was invoked and report was set
	assert.NotNil(t, sess.LastAutoTestReport, "LastAutoTestReport should be set when AutoTestSafety is dry-run")

	sess.Close()
}

func TestRun_SkipsAutoTestPhaseWhenOff(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "verify service health",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
	})
	require.NoError(t, err)

	sess.AutoTestSafety = "off"
	err = sess.Run(context.Background())
	require.NoError(t, err, "Run should complete without error")

	// Verify AutoTest phase was skipped
	assert.Nil(t, sess.LastAutoTestReport, "LastAutoTestReport should be nil when AutoTestSafety is off")

	sess.Close()
}

func TestRun_SkipsAutoTestPhaseWhenEmpty(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := llm.NewMockClient(fullRunResponses())
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "verify service health",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
	})
	require.NoError(t, err)

	// AutoTestSafety defaults to empty string
	sess.AutoTestSafety = ""
	err = sess.Run(context.Background())
	require.NoError(t, err, "Run should complete without error")

	// Verify AutoTest phase was skipped
	assert.Nil(t, sess.LastAutoTestReport, "LastAutoTestReport should be nil when AutoTestSafety is empty")

	sess.Close()
}

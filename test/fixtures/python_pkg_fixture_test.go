package fixtures

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestPythonPkgFixture(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available, skipping Python fixture test")
	}

	// Get absolute path to python-pkg fixture (tests run from fixtures directory)
	absProjectDir, err := filepath.Abs("python-pkg")
	require.NoError(t, err, "Failed to get absolute path to python-pkg fixture")

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := llm.NewMockClient(MockResponses("mymath.py"))

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test python fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: absProjectDir,
	})
	require.NoError(t, err)

	// Enable AutoTest phase so coverage_python_parse and gen_python_extract are actually called
	sess.AutoTestSafety = "dry-run"

	err = sess.Run(context.Background())
	// Python fixture tests whether cerberus can process a Python project.
	// If coverage_python_parse or gen_python has bugs, this will fail —
	// fix them TDD-style (write test for the bug, fix, re-run).
	require.NoError(t, err, "Python session should complete")

	assert.NotNil(t, sess.Contract)

	// Verify AutoTest ran and found gaps (mul function is uncovered)
	assert.NotNil(t, sess.LastAutoTestReport, "AutoTest report should exist in dry-run mode")
	assert.Greater(t, len(sess.LastAutoTestReport.Gaps), 0, "Should find coverage gaps (mul function)")
	assert.Greater(t, len(sess.LastAutoTestReport.Generated), 0, "Should generate tests for gaps")
}

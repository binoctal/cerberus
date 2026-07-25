package fixtures

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestNodeAppFixture(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available, skipping Node fixture test")
	}

	// Get absolute path to node-app fixture (tests run from fixtures directory)
	absProjectDir, err := filepath.Abs("node-app")
	require.NoError(t, err, "Failed to get absolute path to node-app fixture")

	// Check if node_modules exists; if not, skip rather than fail in CI
	nodeModulesDir := filepath.Join(absProjectDir, "node_modules")
	if _, err := os.Stat(nodeModulesDir); os.IsNotExist(err) {
		t.Skip("node_modules absent in test/fixtures/node-app, run 'npm install' in that directory to enable fixture test")
	}

	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../migrations"))

	cfg := project.DefaultConfig()
	cfg.Settings.Mode = "local"
	mockClient := MockClient("lib.js")

	sess, err := session.NewSession(context.Background(), session.SessionConfig{
		Mode:       session.ModeRun,
		Goal:       "test node fixture",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     zap.NewNop(),
		ProjectDir: absProjectDir,
	})
	require.NoError(t, err)

	// Enable AutoTest phase so coverage_node_parse and gen_node_extract are actually called
	sess.AutoTestSafety = "dry-run"

	err = sess.Run(context.Background())
	// Node fixture tests whether cerberus can process a Node project.
	// If coverage_node_parse or gen_node has bugs, this will fail —
	// fix them TDD-style (write test for the bug, fix, re-run).
	require.NoError(t, err, "Node session should complete")

	assert.NotNil(t, sess.Contract)

	// Verify AutoTest ran and found gaps (mul function is uncovered)
	assert.NotNil(t, sess.LastAutoTestReport, "AutoTest report should exist in dry-run mode")
	assert.Greater(t, len(sess.LastAutoTestReport.Gaps), 0, "Should find coverage gaps (mul function)")
	assert.Greater(t, len(sess.LastAutoTestReport.Generated), 0, "Should generate tests for gaps")
}

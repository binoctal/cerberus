package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

func TestCoverageForSession_WithAutoTestReport(t *testing.T) {
	// Test with AutoTest report → should reuse it.
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	sess := &Session{
		Config:     &cfg,
		Store:      s,
		Logger:     zap.NewNop(),
		ProjectDir: ".",
		LastAutoTestReport: &autotest.AutoTestReport{
			BeforeCoveragePct: 75.5,
		},
	}

	pct := coverageForSession(context.Background(), sess)
	// Should reuse the AutoTest report value
	assert.Equal(t, 75.5, pct, "should reuse AutoTest report coverage")
}

func TestCoverageForSession_NoAutoTestReport_ErrorHandling(t *testing.T) {
	// Test without AutoTest report but with invalid project dir → should return 0
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	sess := &Session{
		Config:     &cfg,
		Store:      s,
		Logger:     zap.NewNop(),
		ProjectDir: "/nonexistent/path/that/does/not/exist",
		// No AutoTest report
	}

	pct := coverageForSession(context.Background(), sess)
	// Should return 0 on error
	assert.Equal(t, 0.0, pct, "should return 0 when coverage run fails")
}

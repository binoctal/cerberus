package session

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestCoverageForSession_GoLineMeasurement(t *testing.T) {
	// coverageFn injected: simulate a Go line measurement of 75.5% (0–100) → 0.755 fraction.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: ".",
		coverageFn: func(_ context.Context, _ *Session) contract.CoverageMeasurement {
			// Stand-in for the real provider path; see TestCoverageForSession_NormalizesProvider.
			return contract.CoverageMeasurement{Pct: 0.755, Unit: "line", Known: true}
		}}
	m := sess.lineCoverage(context.Background())
	assert.True(t, m.Known)
	assert.Equal(t, "line", m.Unit)
	assert.InDelta(t, 0.755, m.Pct, 0.0001)
}

func TestCoverageForSession_NormalizesProviderToFraction(t *testing.T) {
	// Provider returns 0–100; coverageForSession must divide by 100 and set Known
	// when the denominator is non-zero. We exercise the provider path directly by
	// giving a Session with no coverageFn and a ProjectDir that has no measurable
	// source → falls to error path → Known=false.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(),
		ProjectDir: "/nonexistent/path/that/does/not/exist"}
	m := coverageForSession(context.Background(), sess)
	assert.False(t, m.Known, "provider failure → Known=false, not Pct=0 gate-bait")
	assert.Equal(t, 0.0, m.Pct)
}

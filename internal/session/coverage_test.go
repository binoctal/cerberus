package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
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

func TestCoverageForSession_ScalesProviderPctToFraction(t *testing.T) {
	// Drive coverageForSession through the REAL provider path so the
	// Pct: pct100 / 100 normalization (the core scale-bug fix) is exercised
	// directly, not bypassed by coverageFn. We build a tiny isolated Go module
	// whose coverage is deterministic: 4 single-statement functions, 3 exercised
	// by the test → exactly 75.0% line coverage, which coverageForSession must
	// scale to the 0–1 fraction 0.75. If the /100 division is dropped, Pct
	// becomes 75.0 and this assertion fails.
	dir := t.TempDir()
	writeFile := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	writeFile("go.mod", "module covsample\n\ngo 1.25\n")
	writeFile("sample.go",
		"package covsample\n\n"+
			"func A() int { return 1 }\n"+
			"func B() int { return 2 }\n"+
			"func C() int { return 3 }\n"+
			"func D() int { return 4 }\n")
	writeFile("sample_test.go",
		"package covsample\n\n"+
			"import \"testing\"\n\n"+
			"func TestABC(t *testing.T) { _, _, _ = A(), B(), C() }\n")

	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: dir}

	m := coverageForSession(context.Background(), sess)
	assert.True(t, m.Known, "real provider success → Known=true")
	assert.Equal(t, "line", m.Unit)
	assert.InDelta(t, 0.75, m.Pct, 0.001, "provider 75.0% must scale to 0.75 fraction")
}

func TestLineCoverageReport_OverrideReturnsNilReport(t *testing.T) {
	// When coverageFn is injected (tests), the override supplies a measurement
	// only — there is no raw CoverageReport to reuse, so lineCoverageReport
	// returns (nil, measurement). Callers tolerate a nil report.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: ".",
		coverageFn: func(_ context.Context, _ *Session) contract.CoverageMeasurement {
			return contract.CoverageMeasurement{Pct: 0.5, Unit: "line", Known: true}
		}}
	report, m := sess.lineCoverageReport(context.Background())
	assert.Nil(t, report)
	assert.True(t, m.Known)
	assert.InDelta(t, 0.5, m.Pct, 0.0001)
	// lineCoverage must still return only the measurement (unchanged contract).
	assert.Equal(t, m, sess.lineCoverage(context.Background()))
}

func TestLineCoverageReport_RealProviderReturnsReportAndMeasurement(t *testing.T) {
	// On the real provider path, lineCoverageReport returns BOTH the raw report
	// (for gap reuse by the coverage repair loop) and the derived measurement,
	// from a single provider run. Reuse the deterministic covsample fixture.
	dir := t.TempDir()
	writeFile := func(name, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	writeFile("go.mod", "module covsample\n\ngo 1.25\n")
	writeFile("sample.go",
		"package covsample\n\n"+
			"func A() int { return 1 }\n"+
			"func B() int { return 2 }\n"+
			"func C() int { return 3 }\n"+
			"func D() int { return 4 }\n")
	writeFile("sample_test.go",
		"package covsample\n\n"+
			"import \"testing\"\n\n"+
			"func TestABC(t *testing.T) { _, _, _ = A(), B(), C() }\n")

	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: dir}

	report, m := sess.lineCoverageReport(context.Background())
	require.NotNil(t, report, "real provider path returns the raw report for gap reuse")
	assert.NotEmpty(t, report.Profile, "report carries the line profile")
	assert.True(t, m.Known)
	assert.Equal(t, "line", m.Unit)
	assert.InDelta(t, 0.75, m.Pct, 0.001)
}

func TestLineCoverageReport_NilReportOnProviderFailure(t *testing.T) {
	// Provider failure / nothing measurable → (nil, Known:false), not a fake 0.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(),
		ProjectDir: "/nonexistent/path/that/does/not/exist"}
	report, m := sess.lineCoverageReport(context.Background())
	assert.Nil(t, report)
	assert.False(t, m.Known)
}

func TestLineCoverageReport_RunsProviderOnce(t *testing.T) {
	// [R3] one provider run derives both report + measurement. A single
	// lineCoverageReport call must hit coverageFn exactly once; calling it twice
	// (e.g. once for the report, once for the measurement) would over-count.
	s, _ := store.New(":memory:")
	defer func() { _ = s.Close() }()
	cfg := project.DefaultConfig()
	calls := 0
	sess := &Session{Config: &cfg, Store: s, Logger: zap.NewNop(), ProjectDir: ".",
		coverageFn: func(_ context.Context, _ *Session) contract.CoverageMeasurement {
			calls++
			return contract.CoverageMeasurement{Pct: 0.5, Unit: "line", Known: true}
		}}
	_, _ = sess.lineCoverageReport(context.Background())
	assert.Equal(t, 1, calls, "single lineCoverageReport → exactly one provider run")
}

package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
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

// TestExercisedEdges verifies the path-coverage core: a declared message edge is
// "exercised" when a case's evidence shows its sender role sent the type and a
// recipient role received it. connectionID→role comes from the case's ws_connect
// steps; unmatched connections are excluded (conservative under-count).
func TestExercisedEdges(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-bridge", MatchedType: "device:online"},
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true},
		},
	}}
	exercised, _ := exercisedEdges(results, required)
	// device:online bridge→web exercised; session:send web→bridge NOT.
	key := func(e project.VocabEdge) string { return e.FromRole + "|" + e.ToRole + "|" + e.Type }
	if !exercised[key(required[0])] {
		t.Errorf("expected device:online bridge->web exercised")
	}
	if exercised[key(required[1])] {
		t.Errorf("session:send web->bridge must NOT be exercised")
	}
}

// TestExercisedEdges_PushProtocolReceiveDriven locks the receive-driven,
// vocab-attributed model: a bridge→web signal that is SERVER-PUSHED on peer
// join (no explicit ws_send of it) is still counted as exercised when the web
// role observes it. Under a send-side correlation model this would measure 0
// (the open-agents case); the vocab's FromRole attributes the receive to the
// declared bridge→web edge.
func TestExercisedEdges_PushProtocolReceiveDriven(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	// device:online arrives on web with NO preceding ws_send (server push on join).
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true},
		},
	}}
	exercised, _ := exercisedEdges(results, required)
	key := func(e project.VocabEdge) string { return e.FromRole + "|" + e.ToRole + "|" + e.Type }
	if !exercised[key(required[0])] {
		t.Fatalf("push signal device:online bridge->web must be exercised via receive-driven attribution; got %v", exercised)
	}
	if exercised[key(required[1])] {
		t.Errorf("session:send web->bridge must NOT be exercised (no observe)")
	}
	// A negative (ExpectAbsent) or unmatched receive must not attribute an edge.
	results[0].Evidence = []agent.Evidence{
		{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true, ExpectAbsent: true},
		{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "session:send", Matched: false},
	}
	exercised2, _ := exercisedEdges(results, required)
	if len(exercised2) != 0 {
		t.Fatalf("ExpectAbsent and unmatched receives must not attribute edges; got %v", exercised2)
	}
}

// TestPathCoverage locks the message-edge path coverage measurement: empty
// required vocab is unmeasured (Known=false); the brief's fixture exercises 1
// of 2 declared edges → Pct=0.5, Unit="path", Known=true.
func TestPathCoverage(t *testing.T) {
	// Empty required vocab → unmeasured, not a fake 0.
	m := pathCoverage(nil, nil)
	assert.False(t, m.Known, "no declared edges → Known=false")

	// Brief fixture: device:online bridge→web exercised, session:send web→bridge not.
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-bridge", MatchedType: "device:online"},
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true},
		},
	}}
	m = pathCoverage(results, required)
	assert.True(t, m.Known)
	assert.Equal(t, "path", m.Unit)
	assert.InDelta(t, 0.5, m.Pct, 0.0001)

	// Measured-zero branch: non-empty required but nothing exercised → Known:true, Pct 0
	// (distinct from empty required ⇒ Known:false / unmeasured).
	zeroRequired := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	zeroM := pathCoverage(nil, zeroRequired)
	assert.True(t, zeroM.Known, "non-empty required + nothing exercised → Known=true (measured 0%)")
	assert.Equal(t, "path", zeroM.Unit)
	assert.InDelta(t, 0.0, zeroM.Pct, 0.0001)
}

// TestPathCoverage_OutOfBandDoesNotInflate locks the intersection fix: an
// exercised edge that is NOT in the required set (out-of-band type or role-pair)
// must NOT count toward coverage. The old non-intersecting code counted every
// exercised edge, letting out-of-band evidence push Pct to 1.0 (false Reached).
// required has two edges; case evidence exercises ONE required edge plus one
// out-of-band edge → Pct must be 0.5, NOT 1.0. The two exchanges live in separate
// cases so exercisedEdges' per-case type→sender map does not collapse them.
func TestPathCoverage_OutOfBandDoesNotInflate(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled"},
	}
	results := []agent.StepResult{{
		// Required edge #1: bridge→web device:online.
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-bridge", MatchedType: "device:online"},
			{Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:online", Matched: true},
		},
	}, {
		// Out-of-band: web→bridge device:online is NOT in required (wrong direction/type pairing).
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
			{Action: "ws_connect", ConnectionID: "c-bridge", Role: "bridge"},
		}},
		Evidence: []agent.Evidence{
			{Action: "ws_send", ConnectionID: "c-web", MatchedType: "device:online"},
			{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "device:online", Matched: true},
		},
	}}
	m := pathCoverage(results, required)
	assert.True(t, m.Known)
	assert.Equal(t, "path", m.Unit)
	assert.InDelta(t, 0.5, m.Pct, 0.0001, "out-of-band edge must not inflate coverage (1 of 2 required hit)")
}

// TestSessionHasVocab locks the structural accessor: a session has a vocab when
// ANY service declares a non-nil Vocabulary with at least one edge. Structural,
// not mode-based — a service with only non-message_handled or flagged edges still
// counts as having a vocab (Edges non-empty), while nil/empty vocabularies do not.
func TestSessionHasVocab(t *testing.T) {
	cfg := project.DefaultConfig()
	sess := &Session{Config: &cfg}
	assert.False(t, sessionHasVocab(sess), "no services → no vocab")

	cfg.Services = append(cfg.Services,
		project.Service{Name: "no-vocab"},
		project.Service{Name: "empty-vocab", Vocabulary: &project.Vocabulary{}},
	)
	assert.False(t, sessionHasVocab(sess), "nil/empty vocabularies → no vocab")

	cfg.Services = append(cfg.Services, project.Service{
		Name: "has-vocab",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		}},
	})
	assert.True(t, sessionHasVocab(sess), "any service with non-empty Edges → has vocab")
}

// TestRequiredEdges locks the required-surface filter: only message_handled,
// non-unsupported, non-partial edges are collected. Other triggers and flagged
// edges are excluded; nil-vocab services are skipped entirely.
func TestRequiredEdges(t *testing.T) {
	cfg := project.DefaultConfig()
	cfg.Services = []project.Service{
		{Name: "no-vocab"},
		{Name: "skip-non-message-handled", Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "web", ToRole: "bridge", Type: "session:open", Trigger: "connect_web"},
		}}},
		{Name: "keep", Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "message_handled", Partial: true},
			{FromRole: "web", ToRole: "bridge", Type: "session:broadcast", Trigger: "message_handled", Unsupported: true},
			{FromRole: "bridge", ToRole: "web", Type: "device:offline", Trigger: "message_handled"},
		}}},
	}
	sess := &Session{Config: &cfg}
	edges := requiredEdges(sess)
	require.Len(t, edges, 2, "only message_handled, non-flagged edges are kept")
	assert.Equal(t, "device:online", edges[0].Type)
	assert.Equal(t, "device:offline", edges[1].Type)
}

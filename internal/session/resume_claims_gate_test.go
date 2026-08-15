package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// newClaimsResumeSession builds a Session over a migrations-backed store with
// the claims-gate fixture config plus a critical claim ledger, for exercising
// the resume path's claims reconciliation.
func newClaimsResumeSession(t *testing.T, claims []project.Claim) (*store.Store, *Session) {
	t.Helper()
	s := testStoreWithMigrations(t)
	cfg := claimsGateFixture()
	cfg.Claims = &project.ClaimsFile{Claims: claims}
	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "claims resume",
		Config:     cfg,
		Store:      s,
		Client:     llm.NewMockClient(nil),
		Logger:     zap.NewNop(),
		ProjectDir: ".",
	})
	require.NoError(t, err)
	return s, sess
}

// persistVerdict records a finished trace plus its verdict for target, the
// minimum the store needs to mark a case completed pre-interruption.
func persistVerdict(t *testing.T, s *store.Store, sess *Session, target, status string) {
	t.Helper()
	traceID, err := s.CreateTrace(context.Background(), sess.ID, "ws", target)
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(context.Background(), traceID, status))
	_, err = s.CreateVerdict(context.Background(), sess.ID, traceID, target, status, 0.9, "judge", "ok", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)
}

// TestResumeClaimsGate_FullEvidenceNoFalseGate pins review finding 1: resume
// reconciliation must run against the session's COMPLETE evidence. The
// critical claim's only evidence is a pre-interruption case (removed from the
// resume plan by filterRemainingCases); reconciling this resume's partial
// results alone would degrade the claim and falsely trip the gate.
func TestResumeClaimsGate_FullEvidenceNoFalseGate(t *testing.T) {
	s, sess := newClaimsResumeSession(t, []project.Claim{
		{ID: "schedule-real-cli", Text: "调度真实 AI CLI 执行任务", Critical: true},
	})
	defer func() { _ = s.Close() }()

	plan := agent.TestPlan{
		Goal: "claims resume", ProjectURL: "ws://localhost:9999",
		Cases: []agent.TestCase{
			// Pre-interruption: the only real-tier evidence for the claim.
			{ID: "tc-done", Name: "real bridge flow", Target: "ws-flow-device",
				Claims: []string{"schedule-real-cli"},
				Steps:  []agent.TestStep{{Action: "ws_connect", Role: "device"}}},
			// Still pending: executed during this resume, unbound emulated.
			{ID: "tc-pending", Name: "pending", Target: "ws-flow-web", Method: "GET"},
		},
	}
	require.NoError(t, s.SavePlan(context.Background(), sess.ID, plan))
	persistVerdict(t, s, sess, "ws-flow-device", "pass")

	ctx := context.Background()
	rp := &resumePhase{session: sess, ctx: ctx, startTime: time.Now()}
	require.NoError(t, rp.loadSavedPlan())
	require.NoError(t, rp.filterRemainingCases())

	// The completed case's StepResult was reconstructed from plan + verdicts.
	require.Len(t, rp.prior, 1)
	assert.Equal(t, "tc-done", rp.prior[0].TestCase.ID)
	assert.Equal(t, agent.StepPassed, rp.prior[0].Status)
	assert.Equal(t, []string{"schedule-real-cli"}, rp.prior[0].TestCase.Claims,
		"the reconstructed TestCase carries the claim binding")

	// This resume only produced the pending case's result.
	rp.results = []agent.StepResult{{
		TestCase: &agent.TestCase{ID: "tc-pending", Target: "ws-flow-web"},
		Status:   agent.StepPassed,
	}}
	rp.buildSummary()

	assert.Equal(t, 1, rp.summary.ClaimsProven,
		"prior real-tier evidence proves the critical claim")
	assert.False(t, rp.summary.ClaimsGateTriggered, "no false gate from partial resume results")
	assert.NoError(t, gateErrorIfFailed(rp.summary))
}

// TestResumeClaimsGate_PriorFailStillGates: full-evidence reconciliation must
// not swing fail-open — a pre-interruption FAILED binding leaves the critical
// claim unevidenced and the gate must still bite.
func TestResumeClaimsGate_PriorFailStillGates(t *testing.T) {
	s, sess := newClaimsResumeSession(t, []project.Claim{
		{ID: "schedule-real-cli", Text: "调度真实 AI CLI 执行任务", Critical: true},
	})
	defer func() { _ = s.Close() }()

	plan := agent.TestPlan{
		Goal: "claims resume", ProjectURL: "ws://localhost:9999",
		Cases: []agent.TestCase{
			{ID: "tc-done", Name: "real bridge flow", Target: "ws-flow-device",
				Claims: []string{"schedule-real-cli"},
				Steps:  []agent.TestStep{{Action: "ws_connect", Role: "device"}}},
			{ID: "tc-pending", Name: "pending", Target: "ws-flow-web", Method: "GET"},
		},
	}
	require.NoError(t, s.SavePlan(context.Background(), sess.ID, plan))
	persistVerdict(t, s, sess, "ws-flow-device", "fail")

	ctx := context.Background()
	rp := &resumePhase{session: sess, ctx: ctx, startTime: time.Now()}
	require.NoError(t, rp.loadSavedPlan())
	require.NoError(t, rp.filterRemainingCases())
	require.Len(t, rp.prior, 1)
	assert.Equal(t, agent.StepFailed, rp.prior[0].Status)

	rp.results = []agent.StepResult{{
		TestCase: &agent.TestCase{ID: "tc-pending", Target: "ws-flow-web"},
		Status:   agent.StepPassed,
	}}
	rp.buildSummary()

	assert.Equal(t, 1, rp.summary.ClaimsUnevidenced)
	assert.True(t, rp.summary.ClaimsGateTriggered)
	assert.ErrorIs(t, gateErrorIfFailed(rp.summary), ErrClaimsGate)
}

// TestResumeClaimsGate_AllCompletedGates pins review finding 2: the "all cases
// already completed" early return must still arbitrate claims. An unproven
// critical claim (emulated-only evidence) makes the resumed session incomplete
// and Resume returns ErrClaimsGate — never a silent completed/exit 0.
func TestResumeClaimsGate_AllCompletedGates(t *testing.T) {
	s, sess := newClaimsResumeSession(t, []project.Claim{
		{ID: "multi-device", Text: "支持多设备", Critical: true},
	})
	defer func() { _ = s.Close() }()
	// Emulate-only evidence: drop the real-process actor so the bound case
	// cannot reach the real tier (and resume launches no external process).
	sess.Config.Actors[1].Fidelity = project.FidelityEmulated

	plan := agent.TestPlan{
		Goal: "claims resume", ProjectURL: "ws://localhost:9999",
		Cases: []agent.TestCase{
			{ID: "tc-web", Name: "emulated flow", Target: "ws-flow-web",
				Claims: []string{"multi-device"},
				Steps:  []agent.TestStep{{Action: "ws_connect", Role: "web"}}},
		},
	}
	require.NoError(t, s.SavePlan(context.Background(), sess.ID, plan))
	persistVerdict(t, s, sess, "ws-flow-web", "pass")

	err := sess.Resume(context.Background())
	assert.ErrorIs(t, err, ErrClaimsGate)
	sess.Close()

	dbSess, getErr := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, getErr)
	assert.Equal(t, "incomplete", dbSess.Status,
		"the gate marks the all-completed resume incomplete, not completed/failed")
}

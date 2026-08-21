package session

import (
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// PullFindings backfills findings for a PERSISTED session: the plan supplies
// each TestCase (claim bindings, tier inputs), the verdicts supply the final
// status per CASE (verdicts carry case_id since V013; a target is NOT a case
// key — deterministic generators share one target across many cases). Legacy
// verdict rows without a case id cannot be mapped and are skipped. A case id
// with verdicts but no passing one is a failed case. Same upsert semantics as
// backflowFindings.
func PullFindings(projectDir string, cfg *project.Config, plan *agent.TestPlan, verdicts []store.Verdict, sessionID string, log *zap.Logger) error {
	if plan == nil || len(verdicts) == 0 {
		return nil
	}
	passing := map[string]bool{}
	failing := map[string]store.Verdict{}
	for _, v := range verdicts {
		if v.CaseID == "" {
			continue // legacy row (pre-V013): no per-case identity to map
		}
		if v.Status == string(agent.StepPassed) {
			passing[v.CaseID] = true
			continue
		}
		if _, seen := failing[v.CaseID]; !seen {
			failing[v.CaseID] = v
		}
	}
	var failed []agent.StepResult
	for i := range plan.Cases {
		tc := plan.Cases[i]
		v, isFailed := failing[tc.ID]
		if !isFailed || passing[tc.ID] {
			continue
		}
		failed = append(failed, agent.StepResult{TestCase: &tc, Status: agent.StepFailed, Error: errFromVerdict(v)})
	}
	if len(failed) == 0 {
		return nil
	}
	// verdicts=nil is fine here: the failed list is already filtered against
	// passing DB verdicts above, so no case needs a second verdict check.
	backflowFindings(projectDir, cfg, failed, nil, sessionID, log)
	return nil
}

// errFromVerdict renders a scannable one-line failure reason from a persisted
// verdict: the root-cause field when present, else the examiner reasoning,
// else a bare marker (never empty — a finding's summary is required).
func errFromVerdict(v store.Verdict) error {
	s := string(v.FailureReason)
	if s == "" {
		s = v.Reasoning
	}
	if s == "" {
		s = "failed (no recorded reason)"
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > findingsSummaryCap {
		s = s[:findingsSummaryCap]
	}
	return &findingError{s}
}

// findingError lets a plain string travel in StepResult.Error.
type findingError struct{ msg string }

func (e *findingError) Error() string { return e.msg }

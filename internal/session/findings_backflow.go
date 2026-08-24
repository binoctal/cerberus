package session

import (
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/project"
)

// findingsSummaryCap bounds a finding's summary: one line, readable in a
// list render.
const findingsSummaryCap = 160

// backflowFindings records every FAILED case of a completed session into
// .cerberus/findings.yaml (observed defects beside the claims ledger —
// findings are observations, claims are promises; no gate interaction).
// Identity is case+error-signature, so the same failure across sessions
// bumps count instead of piling up. Independent of the claims ledger: a
// ledger-less project still gets findings.
// A step failure is NOT recorded when the examiner's final verdict for the
// case passes (negative cases: the executor's own gate is false for the
// expected 4xx while the judge confirms the expectation was met) or when a
// fallback/replacement rescued the primary — neither leaves an observed
// defect. verdicts may be nil (PullFindings pre-filters via DB verdicts).
func backflowFindings(projectDir string, cfg *project.Config, results []agent.StepResult, verdicts []examiner.FinalVerdict, sessionRef string, log *zap.Logger) {
	passedByCase := make(map[string]bool, len(verdicts))
	for _, v := range verdicts {
		if tc := v.StepResult.TestCase; tc != nil && v.Status == examiner.StatusPass {
			passedByCase[tc.ID] = true
		}
	}
	// Same reclassification the summary applies: a rescued primary is not a
	// failed unit (computeRecoveredPrimaries is Agent-side evidence only).
	recoveredPrimary, _ := computeRecoveredPrimaries(results)

	var failed []agent.StepResult
	for _, r := range results {
		if r.Status != agent.StepFailed || r.TestCase == nil {
			continue
		}
		if passedByCase[r.TestCase.ID] || recoveredPrimary[r.TestCase.ID] {
			continue
		}
		failed = append(failed, r)
	}
	if len(failed) == 0 {
		return
	}
	ff, err := project.LoadFindings(projectDir)
	if err != nil {
		log.Warn("findings backflow: load failed", zap.Error(err))
		return
	}
	if ff == nil {
		ff = &project.FindingsFile{}
	}
	realRoleActors := collectRealIdentities(cfg)
	now := time.Now().UTC().Format(time.RFC3339)
	created := 0
	for _, r := range failed {
		created += boolToInt(project.UpsertFinding(ff, project.FindingInput{
			CaseRef:      r.TestCase.ID,
			ErrorSummary: findingSummary(r),
			SessionRef:   sessionRef,
			ClaimRefs:    r.TestCase.Claims,
			Tier:         caseEvidenceTier(*r.TestCase, realRoleActors.Roles, realRoleActors.flatIDs()),
			Now:          now,
		}))
	}
	if err := project.SaveFindings(projectDir, ff); err != nil {
		log.Warn("findings backflow: save failed", zap.Error(err))
		return
	}
	log.Info("findings backflow", zap.Int("failed_cases", len(failed)), zap.Int("new_findings", created))
}

// findingSummary renders one scannable line for the failing step: the step
// error when present, else the executor result summary.
func findingSummary(r agent.StepResult) string {
	s := ""
	if r.Error != nil {
		s = r.Error.Error()
	} else if r.Result != nil {
		s = r.Result.Summary()
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > findingsSummaryCap {
		s = s[:findingsSummaryCap]
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

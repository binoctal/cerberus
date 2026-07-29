package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// SessionSummary collects statistics across all phases of a session.
type SessionSummary struct {
	Goal       string `json:"goal"`
	ProjectURL string `json:"project_url"`

	// Scout phase.
	EndpointsFound   int `json:"endpoints_found"`
	TestCasesPlanned int `json:"test_cases_planned"`

	// Agent phase.
	TotalCases int `json:"total_cases"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Uncertain  int `json:"uncertain"`
	// Recovered counts roles rescued by a lazy fallback (A1 Phase 2). A
	// recovered primary is reclassified out of Failed; its fallback result is
	// not an independent unit. Recovered counts toward coverage.
	Recovered int `json:"recovered"`

	// Examiner phase.
	Verdicts          int `json:"verdicts"`
	PendingReview     int `json:"pending_review"`
	ReflectionsStored int `json:"reflections_stored"`

	// Coverage.
	CoveragePct float64 `json:"coverage_pct"`

	// Coverage contract and assessment (optional, if coverage enabled)
	Contract   *contract.Contract   `json:"contract,omitempty"`
	Assessment *contract.Assessment `json:"assessment,omitempty"`

	// Resource usage.
	TotalTokens int    `json:"total_tokens"`
	Duration    string `json:"duration"`
	DurationMs  int64  `json:"duration_ms"`
}

// plannedCaseCount returns the number of real role units in a plan, excluding
// lazy fallback cases (FallbackFor != ""), which are rescue copies of an
// existing primary rather than independent planned roles (A1 Phase 2).
func plannedCaseCount(plan *agent.TestPlan) int {
	if plan == nil {
		return 0
	}
	n := 0
	for i := range plan.Cases {
		if plan.Cases[i].FallbackFor == "" {
			n++
		}
	}
	return n
}

// FromResults builds a summary from agent and examiner results.
func FromResults(goal, projectURL string, planCases int, results []agent.StepResult, verdicts []examiner.FinalVerdict, reflections int, tokensUsed int, elapsed time.Duration) *SessionSummary {
	s := &SessionSummary{
		Goal:              goal,
		ProjectURL:        projectURL,
		TestCasesPlanned:  planCases,
		Verdicts:          len(verdicts),
		ReflectionsStored: reflections,
		TotalTokens:       tokensUsed,
		Duration:          elapsed.Round(time.Millisecond).String(),
		DurationMs:        elapsed.Milliseconds(),
	}

	// Pair primary<->fallback via TestCase.FallbackFor, and primary<->replacement
	// via TestCase.Replaces. A fallback/replacement result is not an independent
	// tally unit. A primary that is recovered — either because its fallback
	// recovered (gated on StepResult.Recovered, set only by the FallbackFor
	// activation path) or because a replacement passed (gated on pass-status) —
	// is reclassified OUT of Failed into Recovered. Both recovery modes feed the
	// same recoveredPrimaryIDs set so the counting rule is uniform.
	recoveredPrimaryIDs := map[string]bool{}
	nonUnitResultCount := 0
	for _, r := range results {
		tc := r.TestCase
		if tc == nil {
			continue
		}
		if tc.FallbackFor != "" {
			nonUnitResultCount++
			if r.Recovered {
				recoveredPrimaryIDs[tc.FallbackFor] = true
			}
		} else if tc.Replaces != "" {
			nonUnitResultCount++
			// Recovery here is gated on the Agent-side r.Status (StepPassed),
			// not the examiner verdict v.Status — mirroring the inherited
			// FallbackFor gating (r.Recovered is also Agent-side). So an
			// examiner downgrade of a passed step could in theory still count
			// as recovered; revisit if the FallbackFor analog is ever tightened.
			if r.Status == agent.StepPassed {
				recoveredPrimaryIDs[tc.Replaces] = true
			}
		}
	}
	s.TotalCases = len(results) - nonUnitResultCount

	// Count final outcomes. Prefer examiner verdicts — the final judgment
	// reflects correctness adjustments (e.g. pass→uncertain) that raw step
	// status lacks, and these counts feed user-facing reports. Fall back to
	// step status only when the Examiner didn't run (no verdicts).
	if len(verdicts) > 0 {
		for _, v := range verdicts {
			tc := v.StepResult.TestCase
			if tc != nil && (tc.FallbackFor != "" || tc.Replaces != "") {
				continue // fallback/replacement result, not an independent unit
			}
			if tc != nil && recoveredPrimaryIDs[tc.ID] {
				// Reclassified out of Failed into Recovered (FallbackFor or
				// Replaces recovery — the primary is counted once, as Recovered).
				s.Recovered++
				continue
			}
			switch v.Status {
			case examiner.StatusPass:
				s.Passed++
			case examiner.StatusFail:
				s.Failed++
			case examiner.StatusSkip:
				s.Skipped++
			case examiner.StatusUncertain:
				s.Uncertain++
			}
		}
	} else {
		for _, r := range results {
			tc := r.TestCase
			if tc != nil && (tc.FallbackFor != "" || tc.Replaces != "") {
				continue
			}
			if tc != nil && recoveredPrimaryIDs[tc.ID] {
				// Reclassified out of Failed into Recovered (FallbackFor or
				// Replaces recovery — the primary is counted once, as Recovered).
				s.Recovered++
				continue
			}
			switch r.Status {
			case agent.StepPassed:
				s.Passed++
			case agent.StepFailed:
				s.Failed++
			case agent.StepSkipped:
				s.Skipped++
			case agent.StepUncertain:
				s.Uncertain++
			}
		}
	}

	for _, v := range verdicts {
		if v.NeedsReview() {
			s.PendingReview++
		}
	}

	// Coverage: (passed + recovered) roles / total role units * 100. Recovered
	// counts as covered (the deterministic fallback proved the role viable).
	if s.TotalCases > 0 {
		s.CoveragePct = float64(s.Passed+s.Recovered) / float64(s.TotalCases) * 100
	}

	return s
}

// String returns a human-readable summary.
func (s *SessionSummary) String() string {
	return fmt.Sprintf(`Session Summary:
  Verdicts: %d pass, %d fail, %d skip, %d uncertain, %d recovered
  Pending review: %d
  Reflections stored: %d (failure + success)
  Total tokens: ~%dK
  Duration: %s`,
		s.Passed, s.Failed, s.Skipped, s.Uncertain, s.Recovered,
		s.PendingReview,
		s.ReflectionsStored,
		s.TotalTokens/1000,
		s.Duration)
}

// ToJSON returns the summary as JSON.
func (s *SessionSummary) ToJSON() string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

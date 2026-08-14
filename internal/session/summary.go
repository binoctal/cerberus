package session

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/project"
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

	// Coverage recovery (observability-only, D1 spec §6.6 [R10]). RepairedCoverage
	// is the post-AutoTest-dispatch measurement; CoverageRecovered flags it met the
	// gate. The Agent Assessment.Reached stays unchanged — this is a report
	// annotation, not a verdict change. Persisted via the stats JSON blob.
	RepairedCoverage  *contract.CoverageMeasurement `json:"repaired_coverage,omitempty"`
	CoverageRecovered bool                          `json:"coverage_recovered,omitempty"`

	// Resource usage.
	TotalTokens int    `json:"total_tokens"`
	Duration    string `json:"duration"`
	DurationMs  int64  `json:"duration_ms"`

	// FailureHints is the per-cause breakdown of correctable failures (non-none
	// redispatch_hint on independent Fail units), surfaced so the report shows
	// what the repair loop acted on. Nil when there are no correctable failures.
	FailureHints map[string]int `json:"failure_hints,omitempty"`
	// NonRepairableFailures counts correctable failures (non-none hint) on case
	// types the repair mechanism cannot fix (process_exec, code_*, browser, ...).
	// Surfaced so an operator understands why the repair loop skipped them.
	NonRepairableFailures int `json:"non_repairable_failures,omitempty"`

	// RealActors lists actors declared fidelity: real-process (backed by real
	// external processes this run). Empty when every actor was self-played.
	RealActors []string `json:"real_actors,omitempty"`
	// AllEmulated is true when actors exist and none are real-process — the
	// run summary watermarks this so "coverage 1.0" cannot silently mean
	// "self-played only".
	AllEmulated bool `json:"all_emulated,omitempty"`
}

// FidelityComposition derives the summary fidelity fields from the project
// config. allEmulated is true only when actors exist and none are real-process.
func FidelityComposition(cfg *project.Config) (real []string, allEmulated bool) {
	if cfg == nil {
		return nil, false
	}
	for _, a := range cfg.Actors {
		if a.Fidelity == project.FidelityRealProcess {
			real = append(real, a.Name)
		}
	}
	return real, len(cfg.Actors) > 0 && len(real) == 0
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

	// Pair primary<->fallback / primary<->replacement and find recovered
	// primaries. A fallback/replacement result is not an independent tally unit.
	recoveredPrimaryIDs, nonUnitResultCount := computeRecoveredPrimaries(results)
	s.TotalCases = len(results) - nonUnitResultCount

	// Count final outcomes. Prefer examiner verdicts — the final judgment
	// reflects correctness adjustments (e.g. pass->uncertain) that raw step
	// status lacks, and these counts feed user-facing reports. Fall back to
	// step status only when the Examiner didn't run (no verdicts).
	if len(verdicts) > 0 {
		tallyFromVerdicts(verdicts, recoveredPrimaryIDs, s)
	} else {
		tallyFromResults(results, recoveredPrimaryIDs, s)
	}

	s.PendingReview = countPendingReview(verdicts)

	// Coverage: (passed + recovered) roles / total role units * 100. Recovered
	// counts as covered (the deterministic fallback proved the role viable).
	if s.TotalCases > 0 {
		s.CoveragePct = float64(s.Passed+s.Recovered) / float64(s.TotalCases) * 100
	}

	return s
}

// computeRecoveredPrimaries scans agent results to find primary case IDs that
// were recovered by a fallback or replacement. It returns the recovered-ID set
// and the count of non-unit results (fallback/replacement entries that are not
// independent tally units), so the caller can compute TotalCases.
//
// A primary that is recovered — either because its fallback recovered (gated on
// StepResult.Recovered, set only by the FallbackFor activation path) or because
// a replacement passed (gated on pass-status) — is reclassified OUT of Failed
// into Recovered. Both recovery modes feed the same set so the counting rule is
// uniform.
func computeRecoveredPrimaries(results []agent.StepResult) (map[string]bool, int) {
	recovered := map[string]bool{}
	nonUnit := 0
	for _, r := range results {
		tc := r.TestCase
		if tc == nil {
			continue
		}
		if tc.FallbackFor != "" {
			nonUnit++
			if r.Recovered {
				recovered[tc.FallbackFor] = true
			}
		} else if tc.Replaces != "" {
			nonUnit++
			// Recovery here is gated on the Agent-side r.Status (StepPassed),
			// not the examiner verdict v.Status — mirroring the inherited
			// FallbackFor gating (r.Recovered is also Agent-side). So an
			// examiner downgrade of a passed step could in theory still count
			// as recovered; revisit if the FallbackFor analog is ever tightened.
			if r.Status == agent.StepPassed {
				recovered[tc.Replaces] = true
			}
		}
	}
	return recovered, nonUnit
}

// isNonUnitTestCase reports whether tc is a fallback or replacement result (not
// an independent tally/review unit). Nil-safe.
func isNonUnitTestCase(tc *agent.TestCase) bool {
	return tc != nil && (tc.FallbackFor != "" || tc.Replaces != "")
}

// tallyFromVerdicts counts final outcomes from examiner verdicts into s. It also
// tallies correctable failure causes (FailureHints / NonRepairableFailures) and
// reclassifies recovered primaries out of Failed into Recovered.
func tallyFromVerdicts(verdicts []examiner.FinalVerdict, recoveredPrimaryIDs map[string]bool, s *SessionSummary) {
	for _, v := range verdicts {
		tc := v.StepResult.TestCase
		if isNonUnitTestCase(tc) {
			continue // fallback/replacement result, not an independent unit
		}
		// Tally correctable failure causes (non-none hint on a Fail unit).
		// Recovered primaries still count — their hint is the cause that was
		// repaired.
		if v.Status == examiner.StatusFail && v.RedispatchHint != agent.HintNone {
			if s.FailureHints == nil {
				s.FailureHints = map[string]int{}
			}
			s.FailureHints[string(v.RedispatchHint)]++
			if tc != nil && !isRepairable(tc) {
				// Correctable cause but a case type repair_case cannot emit.
				s.NonRepairableFailures++
			}
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
}

// tallyFromResults counts final outcomes from raw agent step status into s, used
// only when the Examiner did not run. Recovered primaries are reclassified out
// of Failed into Recovered.
func tallyFromResults(results []agent.StepResult, recoveredPrimaryIDs map[string]bool, s *SessionSummary) {
	for _, r := range results {
		tc := r.TestCase
		if isNonUnitTestCase(tc) {
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

// countPendingReview counts verdicts needing human review, skipping non-unit
// (fallback/replacement) results which are not independent review units.
func countPendingReview(verdicts []examiner.FinalVerdict) int {
	n := 0
	for _, v := range verdicts {
		if isNonUnitTestCase(v.StepResult.TestCase) {
			continue
		}
		if v.NeedsReview() {
			n++
		}
	}
	return n
}

// String returns a human-readable summary.
func (s *SessionSummary) String() string {
	return fmt.Sprintf(`Session Summary:
  Verdicts: %d pass, %d fail, %d skip, %d uncertain, %d recovered
  Pending review: %d
  Reflections stored: %d (failure + success)
  Total tokens: ~%dK
  Duration: %s%s%s%s`,
		s.Passed, s.Failed, s.Skipped, s.Uncertain, s.Recovered,
		s.PendingReview,
		s.ReflectionsStored,
		s.TotalTokens/1000,
		s.Duration,
		s.coverageRecoveredLine(),
		s.failureHintsLine(),
		s.nonRepairableLine()) +
		s.fidelityLine()
}

// fidelityLine renders the fidelity composition: an emulated-only watermark
// when every actor was self-played, or the real-process actor list otherwise.
func (s *SessionSummary) fidelityLine() string {
	if s.AllEmulated {
		return "\n  Fidelity: emulated-only (all actors self-played)"
	}
	if len(s.RealActors) > 0 {
		return "\n  Real actors: " + strings.Join(s.RealActors, ", ")
	}
	return ""
}

// nonRepairableLine notes how many correctable failures were skipped because
// their case type is not repairable (process_exec, code, ...). Empty when 0.
func (s *SessionSummary) nonRepairableLine() string {
	if s.NonRepairableFailures == 0 {
		return ""
	}
	return fmt.Sprintf("\n  Non-repairable by type: %d", s.NonRepairableFailures)
}

// failureHintsLine renders the correctable-failure cause breakdown, e.g.
// "Failure causes: 2 endpoint_drift, 1 ws_match". Empty when no correctable
// failures.
func (s *SessionSummary) failureHintsLine() string {
	if len(s.FailureHints) == 0 {
		return ""
	}
	type kv struct {
		hint string
		n    int
	}
	// Deterministic order: descending count, then alphabetical.
	parts := make([]kv, 0, len(s.FailureHints))
	for h, n := range s.FailureHints {
		parts = append(parts, kv{h, n})
	}
	sort.Slice(parts, func(i, j int) bool {
		if parts[i].n != parts[j].n {
			return parts[i].n > parts[j].n
		}
		return parts[i].hint < parts[j].hint
	})
	var b strings.Builder
	b.WriteString("\n  Failure causes:")
	for _, p := range parts {
		fmt.Fprintf(&b, " %d %s,", p.n, p.hint)
	}
	out := b.String()
	return out[:len(out)-1] // trim trailing comma
}

// coverageRecoveredLine renders the observability-only recovery annotation
// (D1 spec §6.6): "Agent coverage X% (not reached) → repaired to Y% (recovered)".
// Empty unless CoverageRecovered with both measurements present. It never alters
// the Agent gate verdict or any case count.
func (s *SessionSummary) coverageRecoveredLine() string {
	if !s.CoverageRecovered || s.RepairedCoverage == nil || s.Assessment == nil {
		return ""
	}
	return fmt.Sprintf("\n  Coverage: %.0f%% (not reached) → repaired to %.0f%% (recovered)",
		s.Assessment.CoveragePct*100, s.RepairedCoverage.Pct*100)
}

// ToJSON returns the summary as JSON.
func (s *SessionSummary) ToJSON() string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

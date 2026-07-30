package session

import (
	"context"
	"time"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// runPhase holds state for a single run phase
type runPhase struct {
	session     *Session
	ctx         context.Context
	startTime   time.Time
	plan        *agent.TestPlan
	results     []agent.StepResult
	verdicts    []examiner.FinalVerdict
	reflections int
	summary     *SessionSummary
	err         error

	// repairPlanFn is the Scout.RepairPlan seam used by executeRepairLoop.
	// nil = use a real Scout head; tests override it for deterministic output.
	repairPlanFn func(ctx context.Context, goal string, failures []repairInput) ([]agent.TestCase, error)

	// coverageGapFn is the coverage gap-discovery seam used by the coverage
	// repair axis (T5/T6). nil = build the language provider and call Gaps +
	// Go NoTestFileGaps; tests inject deterministic gaps so eligibility is
	// unit-testable without a filesystem or provider.
	coverageGapFn func(before *autotest.CoverageReport) []autotest.CoverageGap

	// coverageProvider, when set, is the shared coverage provider used by BOTH
	// the round measurement (measureCoverageReport) and the AutoTest dispatch
	// (buildAutoTest) so the per-round RunCoverage cost is observable in tests.
	// nil = the session language provider / a freshly built one respectively.
	coverageProvider autotest.CoverageProvider

	// autotestGenerator, when set, replaces the language-specific generator in
	// buildAutoTest (tests inject a stub so RepairGaps runs without an LLM).
	autotestGenerator autotest.TestGenerator
}

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
}

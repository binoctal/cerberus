package session

import (
	"context"
	"time"

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
}

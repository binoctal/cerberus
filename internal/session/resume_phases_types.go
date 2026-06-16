package session

import (
	"context"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// resumePhase holds state for resume operations
type resumePhase struct {
	session     *Session
	ctx         context.Context
	startTime   time.Time
	plan        *agent.TestPlan
	results     []agent.StepResult
	verdicts    []examiner.FinalVerdict
	reflections int
	summary     *SessionSummary
	err         error
}

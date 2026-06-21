package agent

import (
	"context"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// stepExecution holds state for executing a single test step
type stepExecution struct {
	loop                *ReActLoop
	ctx                 context.Context
	tc                  *TestCase
	sessionID           string
	traceID             int64
	start               time.Time
	lastResult          types.ExecutorResult
	lastAction          types.TypedAction
	recoverySkipped     bool
	consecutiveTimeouts int
	recoverAttempts     int
	// environmentalSeen records whether ANY attempt's result was an environmental
	// failure (target unreachable). finalizeResult uses it so a case that hit an
	// unreachable target on some attempt is classified environmental even when a
	// later, non-environmental attempt became the final judged result.
	environmentalSeen bool
}

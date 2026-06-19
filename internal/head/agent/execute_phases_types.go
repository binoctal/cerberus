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
}

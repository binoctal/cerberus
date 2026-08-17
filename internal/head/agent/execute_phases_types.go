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
	// consecutiveZeroSteer counts consecutive steer attempts that returned no
	// action tool call (drift). When it reaches driftSkipThreshold the loop
	// finalizes the case as StepSkipped (not StepFailed) so the Examiner can
	// distinguish LLM drift from a real test failure. Reset to 0 whenever
	// steer emits a real action. Transient LLM errors neither increment nor
	// reset the counter — only a real emitted action resets it.
	consecutiveZeroSteer int
	// environmentalSeen records whether ANY attempt's result was an environmental
	// failure (target unreachable). finalizeResult uses it so a case that hit an
	// unreachable target on some attempt is classified environmental even when a
	// later, non-environmental attempt became the final judged result.
	environmentalSeen bool
	// caseParams holds values captured from earlier http_request response
	// bodies (TestStep.Capture), substituted into later steps' URL/Body/Message
	// as {{case.<name>}}. Initialized at construction so maps.Copy always has
	// a destination.
	caseParams map[string]string
}

// driftSkipThreshold is the consecutive-zero-call steer count at which the
// ReAct loop escalates to StepSkipped. Two was chosen so a single flaky empty
// response still gets a retry (matching the legacy single-drift tolerance)
// while sustained drift terminates the case instead of exhausting every
// MaxSteerAttempts attempt and reading as a failure.
const driftSkipThreshold = 2

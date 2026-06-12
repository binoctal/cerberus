package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/types"
)

// WaitExecutor pauses execution for a specified duration.
type WaitExecutor struct{}

// NewWaitExecutor creates a wait executor.
func NewWaitExecutor() *WaitExecutor { return &WaitExecutor{} }

// Execute handles wait actions.
func (e *WaitExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	a, ok := action.(types.WaitAction)
	if !ok {
		return types.ErrorResult{Err: fmt.Sprintf("wait executor: unsupported action %T", action)}
	}
	if a.Duration == "" {
		return types.ErrorResult{Err: "wait: duration is required"}
	}
	d, err := time.ParseDuration(a.Duration)
	if err != nil {
		return types.ErrorResult{Err: fmt.Sprintf("wait: invalid duration %q: %v", a.Duration, err)}
	}
	select {
	case <-time.After(d):
		return types.WaitResult{OK: true, Latency: time.Since(start)}
	case <-ctx.Done():
		return types.ErrorResult{Err: ctx.Err().Error(), Latency: time.Since(start)}
	}
}

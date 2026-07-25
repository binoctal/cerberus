package agent

import (
	"context"

	"github.com/binoctal/cerberus/internal/types"
)

// SteerForLiveTest exposes the unexported steer method so build-tagged live
// tests in package agent_test can drive the migrated steer path against a real
// LLM without widening the production API. This file is only compiled under
// `go test`, so the symbol never leaves the test build. The
// package-agent-internal tests (executor_steer_test.go etc.) keep calling
// steer directly.
var SteerForLiveTest = func(r *ReActLoop, ctx context.Context, tc *TestCase, prevResult types.ExecutorResult, attempt int) (types.TypedAction, bool, error) {
	return r.steer(ctx, tc, prevResult, attempt)
}

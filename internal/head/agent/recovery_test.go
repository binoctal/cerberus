package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

type fakeExecutorResult struct{}

func (fakeExecutorResult) Success() bool                { return false }
func (fakeExecutorResult) Duration() time.Duration      { return 0 }
func (fakeExecutorResult) Summary() string              { return "action failed" }
func (fakeExecutorResult) Evidence() types.EvidenceData { return types.EvidenceData{Type: "none"} }

// newTestRecovery builds a Recovery wired to a mock client and in-memory store.
// It returns the recovery handle and the mock so tests can preset tool-call
// fixtures via SetToolResponse. NewRecovery installs the default trigram
// embedder when given nil; L3 recall still returns nothing because the empty
// in-memory store has no procedural memories to match.
func newTestRecovery(t *testing.T) (*Recovery, *llm.MockClient) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))
	mock := llm.NewMockClient(nil)
	driver := ai.NewDriver(mock, ai.NewTokenBudget(100000, 10000))
	return NewRecovery(driver, s, DefaultReActConfig(), zap.NewNop(), nil), mock
}

// TestRecover_AssemblesActionToolCall verifies the tool-calling recovery path:
// when the LLM emits an action tool call, Recover assembles it to a TypedAction
// and returns RecoverDecision{Action: <assembled>, Skip: false}. Under the new
// mutually-exclusive semantics, an action tool call always yields a non-nil
// Action with Skip=false — the loop's tryRecovery runs the recovered action.
func TestRecover_AssemblesActionToolCall(t *testing.T) {
	rc, mock := newTestRecovery(t)
	mock.SetToolResponse("default", []llm.ToolCall{toolCallFromAction(types.HTTPAction{
		Method: "GET", URL: "/retry",
	})})

	tc := TestCase{ID: "tc", Target: "/retry", Expectation: "e"}
	dec, err := rc.Recover(context.Background(), tc, fakeExecutorResult{}, 1)

	require.NoError(t, err)
	assert.False(t, dec.Skip, "action tool call must not set Skip")
	require.NotNil(t, dec.Action, "action tool call must yield a non-nil Action")
	ha, ok := dec.Action.(types.HTTPAction)
	require.True(t, ok, "recovered action must assemble to HTTPAction")
	assert.Equal(t, "GET", ha.Method)
	assert.Equal(t, "/retry", ha.URL)
}

// TestRecover_SkipToolReturnsSkip verifies the dedicated `skip` control tool:
// when the LLM emits `skip`, Recover returns RecoverDecision{Skip: true} with a
// nil Action. `skip` is exclusive to the recovery tool surface (NOT in
// actionTools() — steer cannot skip); Recover inspects call.Name directly
// rather than delegating to assembleAction, which would otherwise reject it.
func TestRecover_SkipToolReturnsSkip(t *testing.T) {
	rc, mock := newTestRecovery(t)
	mock.SetToolResponse("default", []llm.ToolCall{{Name: "skip", Input: map[string]any{}}})

	tc := TestCase{ID: "tc", Target: "/unrecoverable", Expectation: "e"}
	dec, err := rc.Recover(context.Background(), tc, fakeExecutorResult{}, 1)

	require.NoError(t, err)
	assert.True(t, dec.Skip, "skip tool call must set Skip")
	assert.Nil(t, dec.Action, "skip must not carry an Action (mutually exclusive)")
}

// TestRecover_ZeroToolCallsDriftsToSkip verifies the drift path: when the LLM
// emits no tool calls (the new "drift" state under tool-calling), Recover
// returns RecoverDecision{Skip: true}. This parallels the post-S3 recover
// policy: no diagnosis, no fallback guessing — an empty recovery response
// abandons the target so the loop finalizes as StepSkipped (not StepFailed).
func TestRecover_ZeroToolCallsDriftsToSkip(t *testing.T) {
	rc, mock := newTestRecovery(t)
	// "default" maps to an empty text response with no tool calls.
	mock.SetToolResponse("default", nil)

	tc := TestCase{ID: "tc", Target: "/drift", Expectation: "e"}
	dec, err := rc.Recover(context.Background(), tc, fakeExecutorResult{}, 1)

	require.NoError(t, err, "zero-call drift must surface as a decision, not an error")
	assert.True(t, dec.Skip, "zero tool calls must signal skip")
	assert.Nil(t, dec.Action, "zero tool calls must not synthesize an Action")
}

// TestRecover_TransientErrorFallsBackToSkip preserves today's graceful
// skip-on-failure: a transient LLM error (network, budget exhaustion) returns
// RecoverDecision{Skip: true}, nil error — the loop finalizes the case as
// StepSkipped rather than aborting the whole run. This mirrors the pre-S3
// behavior of treating Decide failure as a soft skip.
func TestRecover_TransientErrorFallsBackToSkip(t *testing.T) {
	rc, _ := newTestRecovery(t)
	rc.driver = ai.NewDriver(errMockClient{}, ai.NewTokenBudget(1, 0)) // budget exhausted → error

	tc := TestCase{ID: "tc", Target: "/x", Expectation: "e"}
	dec, err := rc.Recover(context.Background(), tc, fakeExecutorResult{}, 1)

	require.NoError(t, err, "transient LLM error must surface as a skip decision, not an error")
	assert.True(t, dec.Skip, "transient LLM error must fall back to skip")
	assert.Nil(t, dec.Action)
}

// errMockClient is a minimal llm.Client stub whose Complete always errors,
// used to exercise Recover's transient-error path.
type errMockClient struct{}

func (errMockClient) Complete(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return nil, assert.AnError
}
func (errMockClient) CompleteWithVision(ctx context.Context, prompt string, images [][]byte) (*llm.Response, error) {
	return nil, assert.AnError
}
func (errMockClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.StreamEvent, error) {
	return nil, assert.AnError
}

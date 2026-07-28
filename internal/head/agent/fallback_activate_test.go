package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// fallbackFakeExec drives a ws_flow case down the deterministic runSteps path
// to a non-environmental StepFailed. ws_connect/ws_send/ws_disconnect always
// succeed (WSResult.OK=true). A ws_receive succeeds EXCEPT the first one when
// receiveFailsOnce is set — modeling a sound LLM case whose receive fails once
// at runtime (the primary strands its role) while the re-run deterministic
// fallback recovers it. A failing WSResult summary ("ws error ...") carries no
// environmental signal, so isEnvironmental classifies it as a logic failure.
//
// Single-goroutine access: in both serial and parallel ExecutePlan the primary
// case and its inline-activated fallback run in one worker, so the receive
// counter is never touched concurrently.
type fallbackFakeExec struct {
	receiveFailsOnce bool
	receivesSeen     int
}

func (f *fallbackFakeExec) Execute(ctx context.Context, a types.TypedAction) types.ExecutorResult {
	switch a.(type) {
	case types.WSReceiveAction:
		f.receivesSeen++
		if f.receiveFailsOnce && f.receivesSeen == 1 {
			return types.WSResult{OK: false, Err: "receive timeout"}
		}
		return types.WSResult{OK: true}
	case types.WSConnectAction, types.WSSendAction, types.WSDisconnectAction:
		return types.WSResult{OK: true}
	default:
		return types.ErrorResult{Err: "unsupported action"}
	}
}

func fallbackLoop(t *testing.T, receiveFailsOnce bool) (*ReActLoop, string) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(200000, 10000))
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   NewRuleEngine(nil, nil, "."),
		Executor: &fallbackFakeExec{receiveFailsOnce: receiveFailsOnce},
		Config:   DefaultReActConfig(),
		Logger:   zap.NewNop(),
		Embedder: embed.NewTrigramProvider(embed.DefaultDimension),
	})
	sess, err := s.CreateSession(context.Background(), "run", "test", "")
	require.NoError(t, err)
	return loop, sess.ID
}

// wsFlowCase builds a ws_flow Steps case (connect then receive) — connect
// always succeeds under fallbackFakeExec; the first receive fails iff
// receiveFailsOnce.
func wsFlowCase(id string) TestCase {
	return TestCase{
		ID: id, Action: "ws_flow", Target: "ws://127.0.0.1:1/ws", Service: "rt",
		Steps: []TestStep{
			{Action: "ws_connect", ConnectionID: "web", Role: "web"},
			{Action: "ws_receive", ConnectionID: "web", Type: "device:online"},
		},
	}
}

func TestExecutePlan_ActivatesFallbackOnFailure(t *testing.T) {
	loop, sid := fallbackLoop(t, true) // primary's receive fails once
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	res, err := loop.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)
	require.Len(t, res, 2, "primary + activated fallback")
	assert.Equal(t, StepFailed, res[0].Status, "primary failed")
	assert.Equal(t, StepPassed, res[1].Status, "fallback ran and passed")
	assert.True(t, res[1].Recovered, "fallback marked recovered")
	assert.Equal(t, "tc-fallback", res[1].TestCase.ID)
}

func TestExecutePlan_NoFallbackOnPass(t *testing.T) {
	loop, sid := fallbackLoop(t, false) // primary passes
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	res, err := loop.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)
	require.Len(t, res, 1, "primary only — lazy fallback not activated on pass")
	assert.Equal(t, StepPassed, res[0].Status)
}

func TestParallelExecutePlan_ActivatesFallbackOnFailure(t *testing.T) {
	loop, sid := fallbackLoop(t, true) // primary's receive fails once
	primary := wsFlowCase("tc-primary")
	fallback := wsFlowCase("tc-fallback")
	fallback.FallbackFor = "tc-primary"
	fallback.Priority = -1

	pExec := NewParallelExecutor(loop, ParallelConfig{MaxWorkers: 2}, zap.NewNop())
	res, err := pExec.ExecutePlan(context.Background(), &TestPlan{
		Goal: "g", Cases: []TestCase{primary, fallback},
	}, sid)
	require.NoError(t, err)

	// collectResults returns results keyed by case ID in plan order; the lazy
	// fallback was activated by its primary's worker, so its result is present.
	byID := map[string]StepResult{}
	for _, r := range res {
		byID[r.TestCase.ID] = r
	}
	assert.Equal(t, StepFailed, byID["tc-primary"].Status, "primary failed")
	assert.Contains(t, byID, "tc-fallback", "fallback activated in parallel")
	assert.True(t, byID["tc-fallback"].Recovered, "fallback recovered")
}

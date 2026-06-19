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

// TestRecover_FallsBackOnActionUnmarshalError mirrors the steer fallback: when
// the recovery LLM returns an action whose envelope resolves but whose payload
// is empty (common with non-Claude models), Recover must fall back to a safe
// default action rather than skip the case (skip = the target is never tested).
func TestRecover_FallsBackOnActionUnmarshalError(t *testing.T) {
	s, err := store.New(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.RunMigrations(context.Background(), s.DB(), "../../../migrations"))

	driver := ai.NewDriver(llm.NewMockClient(map[string]string{
		"default": `{"diagnosis":"x","action":{"type":"file_read"}}`,
	}), ai.NewTokenBudget(100000, 10000))
	rc := NewRecovery(driver, s, DefaultReActConfig(), zap.NewNop())

	tc := TestCase{ID: "tc", Target: "internal/llm", Expectation: "e"}
	dec, err := rc.Recover(context.Background(), tc, fakeExecutorResult{}, 1)

	require.NoError(t, err)
	assert.False(t, dec.Skip, "recover must fall back, not skip, on action unmarshal error")
	assert.NotNil(t, dec.Action)
}

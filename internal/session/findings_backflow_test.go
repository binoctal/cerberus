package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestBackflowFindings(t *testing.T) {
	dir := t.TempDir()
	cfg := &project.Config{ // no claims ledger — findings still flow
		Actors: []project.Actor{{Name: "b1", Fidelity: project.FidelityRealProcess}},
	}
	failed := agent.StepResult{
		TestCase: &agent.TestCase{ID: "case-x", Claims: []string{"ws-relay-messaging"}},
		Status:   agent.StepFailed,
		Error:    assert.AnError,
	}
	passed := agent.StepResult{
		TestCase: &agent.TestCase{ID: "case-ok"},
		Status:   agent.StepPassed,
	}

	backflowFindings(dir, cfg, []agent.StepResult{failed, passed}, "sess-1", zap.NewNop())

	ff, err := project.LoadFindings(dir)
	require.NoError(t, err)
	require.NotNil(t, ff, "a failing run must produce a findings document")
	require.Len(t, ff.Findings, 1, "only failed cases flow back")
	f := ff.Findings[0]
	assert.Contains(t, f.ID, "case-x")
	assert.Equal(t, []string{"ws-relay-messaging"}, f.ClaimRefs)
	assert.Equal(t, project.FindingOpen, f.Status)
	assert.Equal(t, 1, f.Count)
	assert.NotEmpty(t, f.FirstSeen)

	// Same failure again: count bumps, still one entry.
	backflowFindings(dir, cfg, []agent.StepResult{failed}, "sess-2", zap.NewNop())
	ff, err = project.LoadFindings(dir)
	require.NoError(t, err)
	require.Len(t, ff.Findings, 1)
	assert.Equal(t, 2, ff.Findings[0].Count)

	// A passing-only run rewrites nothing and leaves the file intact.
	before := ff.Findings[0].Count
	backflowFindings(dir, cfg, []agent.StepResult{passed}, "sess-3", zap.NewNop())
	ff, err = project.LoadFindings(dir)
	require.NoError(t, err)
	require.Len(t, ff.Findings, 1)
	assert.Equal(t, before, ff.Findings[0].Count)
}

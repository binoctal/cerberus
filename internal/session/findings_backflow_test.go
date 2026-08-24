package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
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

	backflowFindings(dir, cfg, []agent.StepResult{failed, passed}, nil, "sess-1", zap.NewNop())

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
	backflowFindings(dir, cfg, []agent.StepResult{failed}, nil, "sess-2", zap.NewNop())
	ff, err = project.LoadFindings(dir)
	require.NoError(t, err)
	require.Len(t, ff.Findings, 1)
	assert.Equal(t, 2, ff.Findings[0].Count)

	// A passing-only run rewrites nothing and leaves the file intact.
	before := ff.Findings[0].Count
	backflowFindings(dir, cfg, []agent.StepResult{passed}, nil, "sess-3", zap.NewNop())
	ff, err = project.LoadFindings(dir)
	require.NoError(t, err)
	require.Len(t, ff.Findings, 1)
	assert.Equal(t, before, ff.Findings[0].Count)
}

// A step failure whose final verdict passes (negative cases: the executor's
// own gate is false for the expected 4xx while the judge confirms the
// expectation was met) must not produce a finding, nor may a fallback-rescued
// primary — neither leaves an observed defect.
func TestBackflowFindings_SuppressesJudgePassedAndRecovered(t *testing.T) {
	dir := t.TempDir()
	cfg := &project.Config{
		Actors: []project.Actor{{Name: "b1", Fidelity: project.FidelityRealProcess}},
	}
	negative := agent.StepResult{
		TestCase: &agent.TestCase{ID: "tc-neg"},
		Status:   agent.StepFailed,
		Error:    assert.AnError,
	}
	rescuedPrimary := agent.StepResult{
		TestCase: &agent.TestCase{ID: "tc-primary"},
		Status:   agent.StepFailed,
		Error:    assert.AnError,
	}
	genuinelyFailed := agent.StepResult{
		TestCase: &agent.TestCase{ID: "tc-broken"},
		Status:   agent.StepFailed,
		Error:    assert.AnError,
	}
	// Fallback rescued the primary (Agent-side Recovered flag set).
	fallback := agent.StepResult{
		TestCase:  &agent.TestCase{ID: "tc-fallback", FallbackFor: "tc-primary"},
		Status:    agent.StepPassed,
		Recovered: true,
	}
	// Examiner overturned the negative case's step failure: expectation met.
	verdicts := []examiner.FinalVerdict{{
		Status:     examiner.StatusPass,
		StepResult: negative,
	}}

	backflowFindings(dir, cfg, []agent.StepResult{negative, rescuedPrimary, fallback, genuinelyFailed},
		verdicts, "sess-1", zap.NewNop())

	ff, err := project.LoadFindings(dir)
	require.NoError(t, err)
	require.NotNil(t, ff)
	require.Len(t, ff.Findings, 1, "only the unrecovered, non-overturned failure flows back")
	assert.Contains(t, ff.Findings[0].ID, "tc-broken")
}

package session

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

func TestPullFindings(t *testing.T) {
	dir := t.TempDir()
	cfg := &project.Config{}
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "case-fail", Target: "t1", Claims: []string{"ws-relay-messaging"}},
		{ID: "case-pass", Target: "t2"},
		{ID: "case-never-ran", Target: "t3"},
	}}
	verdicts := []store.Verdict{
		{Target: "t1", CaseID: "case-fail", Status: "fail", FailureReason: "assert payload.code mismatch"},
		{Target: "t2", CaseID: "case-pass", Status: "pass"},
		// shared-target sibling: same target as case-fail, verdict WITHOUT
		// case id (legacy row) must not be mistaken for either case.
		{Target: "t1", Status: "fail"},
	}

	require.NoError(t, PullFindings(dir, cfg, plan, verdicts, "sess-old", zap.NewNop()))

	ff, err := project.LoadFindings(dir)
	require.NoError(t, err)
	require.NotNil(t, ff)
	require.Len(t, ff.Findings, 1, "only the failed-with-verdict target flows back")
	f := ff.Findings[0]
	assert.Contains(t, f.ID, "case-fail")
	assert.Equal(t, "assert payload.code mismatch", f.Summary)
	assert.Equal(t, "sess-old", f.SessionRef)
	assert.Equal(t, []string{"ws-relay-messaging"}, f.ClaimRefs)
}

func TestErrFromVerdictFallbacks(t *testing.T) {
	assert.Equal(t, "root cause", errFromVerdict(store.Verdict{FailureReason: "root cause", Reasoning: "longer\nreasoning"}).Error())
	assert.Equal(t, "only reasoning", errFromVerdict(store.Verdict{Reasoning: "only reasoning"}).Error())
	assert.Equal(t, "failed (no recorded reason)", errFromVerdict(store.Verdict{}).Error())
}

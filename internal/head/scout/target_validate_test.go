package scout

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestInvalidReason(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.go"), []byte("x"), 0644))

	assert.NotEmpty(t, invalidReason(".", dir), "too broad")
	assert.NotEmpty(t, invalidReason("", dir), "empty")
	assert.Empty(t, invalidReason("http://x.test/api", dir), "URL not validated")
	assert.Empty(t, invalidReason("go test ./...", dir), "command-like (spaces) not statically validated")
	assert.Empty(t, invalidReason("nonexistent-cmd-xyz-123 arg", dir), "command-like not statically validated")
	assert.Empty(t, invalidReason("real.go", dir), "existing file")
	assert.NotEmpty(t, invalidReason("missing.go", dir), "missing file")
}

func TestValidateTargets_DeprioritizesInvalid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "real.go"), []byte("x"), 0644))
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "1", Target: "real.go", Priority: 0.9},
		{ID: "2", Target: "missing.go", Priority: 0.9},
		{ID: "3", Target: ".", Priority: 0.9},
	}}
	s := NewScout(nil, setupTestStore(t), &project.Config{}, zap.NewNop())

	flagged := s.ValidateTargets(plan, dir)

	assert.Equal(t, 2, flagged)
	assert.Equal(t, 0.9, plan.Cases[0].Priority, "valid target kept")
	assert.Equal(t, -1.0, plan.Cases[1].Priority, "missing path deprioritized")
	assert.Equal(t, -1.0, plan.Cases[2].Priority, "too-broad target deprioritized")
}

func TestValidateTargets_WSFlowCaseNotDeprioritizedForEmptyTarget(t *testing.T) {
	dir := t.TempDir()
	// A ws_flow case is a multi-step WS choreography assembled from begin_case +
	// ws_* tool calls; its "target" is the Steps sequence, not a path/URL. An
	// empty Target must NOT be deprioritized — otherwise every LLM-emitted WS
	// choreography case is silently skipped (regression surfaced by the S4
	// dogfood: Scout emitted ws_flow via tool-calling, but target_validate
	// flagged target="" as "empty or too broad" and the case never ran).
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "wf-1", Action: "ws_flow", Target: "", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Role: "web"},
			{Action: "ws_receive", Role: "bridge", Type: "session:start"},
		}},
		{ID: "bad-1", Target: ""}, // non-ws_flow empty target → still deprioritized
	}}
	s := NewScout(nil, setupTestStore(t), &project.Config{}, zap.NewNop())

	flagged := s.ValidateTargets(plan, dir)

	assert.NotEqual(t, -1.0, plan.Cases[0].Priority, "ws_flow choreography case must not be deprioritized for empty target")
	assert.Equal(t, -1.0, plan.Cases[1].Priority, "non-ws_flow empty-target case still deprioritized")
	assert.Equal(t, 1, flagged, "only the non-ws_flow invalid case is flagged")
}

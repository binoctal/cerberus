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
	assert.Empty(t, invalidReason("go test ./...", dir), "go is in PATH")
	assert.NotEmpty(t, invalidReason("nonexistent-cmd-xyz-123 arg", dir), "bad command")
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
	assert.Equal(t, 0.0, plan.Cases[1].Priority, "missing path deprioritized")
	assert.Equal(t, 0.0, plan.Cases[2].Priority, "too-broad target deprioritized")
}

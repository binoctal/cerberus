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

// TestAppendExecutorCases_AppendsWSCasesWhenProtocolDeclared verifies the WS
// wiring: when a service declares a Protocol, appendExecutorCases (the real
// plan-augmentation entry point) surfaces ws_connect / ws_receive cases in
// addition to the language-based executor cases. Exercises the wiring — it
// does NOT call WSCases directly.
func TestAppendExecutorCases_AppendsWSCasesWhenProtocolDeclared(t *testing.T) {
	rootDir := writeGoModMarker(t)
	cfg := &project.Config{
		Code: project.CodeConfig{Root: rootDir},
		Services: []project.Service{{
			Name: "rt",
			Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
				"bridge": {Handshake: &project.RoleHandshake{AwaitType: "devices:sync", Timeout: 5}},
			}},
		}},
	}
	s := &Scout{config: cfg, logger: zap.NewNop()}
	plan := &agent.TestPlan{}

	s.appendExecutorCases(plan, "bridge receives permission:response", nil)

	var connects, receives, others []agent.TestCase
	for _, c := range plan.Cases {
		switch c.Action {
		case "ws_connect":
			connects = append(connects, c)
		case "ws_receive":
			receives = append(receives, c)
		default:
			others = append(others, c)
		}
	}
	require.NotEmpty(t, connects, "ws_connect case must be appended when a protocol is declared")
	require.NotEmpty(t, receives, "ws_receive case must be appended when a protocol is declared")
	// Language-based executor cases (process_build, code_symbols, ...) are
	// still produced for a Go project alongside the WS cases.
	assert.NotEmpty(t, others, "language-based executor cases must still be appended")
}

// TestAppendExecutorCases_NoWSCasesWhenNoProtocolDeclared is the regression
// guard: a config without a declared protocol must produce the same plan
// appendExecutorCases produced before the WS wiring — no ws_connect /
// ws_receive cases, and the language-based cases are byte-identical to
// GenerateExecutorCases(DetectProjectType(root), goal).
func TestAppendExecutorCases_NoWSCasesWhenNoProtocolDeclared(t *testing.T) {
	rootDir := writeGoModMarker(t)
	cfg := &project.Config{
		Code:     project.CodeConfig{Root: rootDir},
		Services: []project.Service{{Name: "api", URL: "http://x"}},
	}
	s := &Scout{config: cfg, logger: zap.NewNop()}
	plan := &agent.TestPlan{}

	s.appendExecutorCases(plan, "test the API", nil)

	for _, c := range plan.Cases {
		assert.NotEqual(t, "ws_connect", c.Action, "no ws_connect case without a declared protocol")
		assert.NotEqual(t, "ws_receive", c.Action, "no ws_receive case without a declared protocol")
	}

	// Regression snapshot: the language-based cases are exactly what
	// GenerateExecutorCases returns — i.e. the WS wiring did not perturb the
	// pre-existing non-WS case set.
	want := GenerateExecutorCases(DetectProjectType(rootDir), "test the API")
	assert.Equal(t, want, plan.Cases, "non-WS case set must be byte-identical to GenerateExecutorCases output")
}

// writeGoModMarker creates a temp directory containing a go.mod so
// DetectProjectType deterministically returns ProjectGo regardless of the
// test runner's CWD.
func writeGoModMarker(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.25\n"),
		0o644,
	))
	return dir
}

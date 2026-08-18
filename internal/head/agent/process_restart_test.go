package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// stubRestarter records calls for process_restart step tests.
type stubRestarter struct {
	calls []string
	err   error
}

func (s *stubRestarter) RestartActor(_ context.Context, actorName string) error {
	s.calls = append(s.calls, actorName)
	return s.err
}

// TestRunStepsProcessRestart_NilHookFails: a process_restart step without a
// harness attached must fail the case with a clear error, not silently pass.
func TestRunStepsProcessRestart_NilHookFails(t *testing.T) {
	tc := TestCase{ID: "tc-restart-1", Target: "ws://unused", Action: "ws_flow", Steps: []TestStep{
		{Action: "process_restart", Role: "bridge2"},
	}}
	se := newStepExecution(t, &tc)
	res := se.runSteps()
	require.Equal(t, StepFailed, res.Status)
	require.Error(t, res.Error)
	require.Contains(t, res.Error.Error(), "no actor restarter")
}

// TestRunStepsProcessRestart_PassAndRoleResolution: the step resolves the
// declared role to its credential_ref actor and passes when the harness
// reports success; the evidence entry carries the resolved actor name.
func TestRunStepsProcessRestart_PassAndRoleResolution(t *testing.T) {
	wsIdx := &WSProtocolIndex{
		ByHost: map[string]*project.Protocol{"unused": {
			Roles: map[string]*project.ProtocolRole{
				"bridge2": {CredentialRef: "bridge-pty-2"},
			},
		}},
	}
	tc := TestCase{ID: "tc-restart-2", Target: "ws://unused", Action: "ws_flow", Steps: []TestStep{
		{Action: "process_restart", Role: "bridge2"},
	}}
	se := newStepExecutionWithIdx(t, &tc, wsIdx)
	rs := &stubRestarter{}
	se.loop.actorRestart = rs
	res := se.runSteps()
	require.Equal(t, StepPassed, res.Status)
	require.Equal(t, []string{"bridge-pty-2"}, rs.calls)
	require.Len(t, res.Evidence, 1)
	require.Equal(t, "process_restart", res.Evidence[0].Action)
	require.Contains(t, res.Evidence[0].Content, "bridge-pty-2")
}

// TestRunStepsProcessRestart_RestarterErrorFails: a harness relaunch failure
// fails the step (the sacrificial bridge never came back).
func TestRunStepsProcessRestart_RestarterErrorFails(t *testing.T) {
	tc := TestCase{ID: "tc-restart-3", Target: "ws://unused", Action: "ws_flow", Steps: []TestStep{
		// Unknown role: the actor name falls back to the literal.
		{Action: "process_restart", Role: "bridge-pty-2"},
	}}
	se := newStepExecution(t, &tc)
	rs := &stubRestarter{err: errors.New("ready pattern not seen")}
	se.loop.actorRestart = rs
	res := se.runSteps()
	require.Equal(t, StepFailed, res.Status)
	require.Equal(t, []string{"bridge-pty-2"}, rs.calls)
	require.Contains(t, res.Error.Error(), "ready pattern not seen")
}

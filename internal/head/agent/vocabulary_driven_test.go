//go:build integration

package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// vocabPath points at the dogfood vocabulary produced by
// `cerberus protocol vocabulary`. Relative to internal/head/agent/, so it
// resolves to repo-root dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml.
const vocabPath = "../../../dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml"

// TestVocabularyDriven builds TestCase tables from open-agents.vocab.yaml at
// run time and asserts each non-unsupported message_handled edge relays
// end-to-end against a live DO. Supersedes the hardcoded rows in
// TestBridgeToWebRelay / TestWebToBridgeRouting once parity is shown (Task 8).
func TestVocabularyDriven(t *testing.T) {
	vocab, err := project.LoadVocabulary(vocabPath)
	if err != nil {
		t.Skipf("vocab not generated (%s): %v", vocabPath, err)
	}
	f := setupOpenAgents(t, false)
	target := "ws://localhost:8989/ws/" + f.userId

	for _, e := range vocab.Edges {
		e := e
		name := fmt.Sprintf("%s_%s_to_%s", e.Trigger, e.FromRole, e.ToRole)
		t.Run(name+"/"+e.Type, func(t *testing.T) {
			if e.Unsupported || e.Partial {
				t.Skipf("edge %q unsupported/partial — finding, not failure", e.Type)
			}
			if e.Trigger != "message_handled" {
				t.Skipf("trigger %q not asserted by v1 (lifecycle)", e.Trigger)
			}
			require.NotEmpty(t, e.FromRole, "message_handled edge needs a from_role")

			// Per-edge choreography + outbound message are built by the shared
			// helper (the single implementation of "how the Agent consumes a
			// vocab edge"), unit-tested in edge_steps_test.go.
			steps, _ := BuildEdgeSteps(e, f.deviceId)
			tc := &TestCase{ID: "tc-vocab-" + e.Type, Target: target, Steps: steps}
			se := newStepExecutionWithIdx(t, tc, f.wsIdx)
			res := se.runSteps()
			for _, ev := range res.Evidence {
				t.Logf("step evidence: %s", ev.Content)
			}
			require.Equal(t, StepPassed, res.Status, "edge %q did not relay", e.Type)
		})
	}
}

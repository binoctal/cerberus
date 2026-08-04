//go:build integration

package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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

			// Connect both roles (handshake await device:online is optional).
			// For web→web broadcast edges the DO excludes the sender from the
			// broadcast (broadcastToWeb(msg, ws)), so a second web client is
			// required to observe the relay.
			steps := []TestStep{
				{Action: "ws_connect", Role: "web", ConnectionID: "c-web"},
				{Action: "ws_connect", Role: "bridge", ConnectionID: "c-bridge"},
				{Action: "ws_receive", ConnectionID: "c-web", Type: "device:online", Timeout: 3},
			}
			if e.FromRole == "web" && e.ToRole == "web" {
				steps = append(steps, TestStep{Action: "ws_connect", Role: "web", ConnectionID: "c-web-2"})
			}
			sender := "c-" + e.FromRole
			receiver := "c-" + e.ToRole
			if e.FromRole == "web" && e.ToRole == "web" {
				receiver = "c-web-2"
			}
			// Build the outbound message. Edges that declare a route_field
			// (e.g. payload.deviceId) require that field present or the DO
			// rejects with MISSING_DEVICE_ID before relaying; the vocab now
			// describes this, so payload shape is driven by RouteField
			// rather than a from_role heuristic.
			msg := fmt.Sprintf(`{"type":%q}`, e.Type)
			if e.RouteField != "" {
				field := strings.TrimPrefix(e.RouteField, "payload.")
				body, err := json.Marshal(map[string]any{
					"type":    e.Type,
					"payload": map[string]any{field: f.deviceId},
				})
				if err != nil {
					t.Fatalf("marshal msg: %v", err)
				}
				msg = string(body)
			}
			steps = append(steps,
				TestStep{Action: "ws_send", ConnectionID: sender, Message: msg},
				TestStep{Action: "ws_receive", ConnectionID: receiver, Type: e.Type, Timeout: 3},
			)
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

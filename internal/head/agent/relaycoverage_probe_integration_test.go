//go:build integration

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// probeVocabEdge is the minimal slice of a vocab edge the probe needs.
type probeVocabEdge struct {
	FromRole string `yaml:"from_role"`
	ToRole   string `yaml:"to_role"`
	Type     string `yaml:"type"`
	Trigger  string `yaml:"trigger"`
}

// readDogfoodVocabEdges loads the ws-realtime dogfood vocabulary and returns
// its edges. The probe mirrors scout.wsRelayCoverageCases (whose correctness is
// unit-locked in the scout package) because an agent-package test cannot import
// scout (scout imports agent → import cycle).
func readDogfoodVocabEdges(t *testing.T) []probeVocabEdge {
	t.Helper()
	b, err := os.ReadFile("../../../dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml")
	require.NoError(t, err, "read dogfood vocab")
	var doc struct {
		Edges []probeVocabEdge `yaml:"edges"`
	}
	require.NoError(t, yaml.Unmarshal(b, &doc), "parse dogfood vocab")
	return doc.Edges
}

// probeEdge runs one 4-step relay (From connect → To connect → From send T →
// To receive T) and reports whether the recipient observed a matched receive
// of T. A matched receive ⇒ client-triggerable (the server relays the type
// from a client send); no match within the receive timeout ⇒ server-only
// candidate (the server emits it only on internal events). Mirrors
// scout.wsRelayCoverageCases' case shape; payload is a bare type envelope.
func probeEdge(t *testing.T, f oaFixture, e probeVocabEdge) bool {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"type": e.Type})
	tc := &TestCase{
		ID:     fmt.Sprintf("probe-%s-%s-%s", e.FromRole, e.ToRole, e.Type),
		Target: "ws://localhost:8989/ws/" + f.userId,
		Action: "ws_flow",
		Steps: []TestStep{
			{Action: "ws_connect", Role: e.FromRole, ConnectionID: "c-from"},
			{Action: "ws_connect", Role: e.ToRole, ConnectionID: "c-to"},
			{Action: "ws_send", ConnectionID: "c-from", Message: string(body)},
			{Action: "ws_receive", ConnectionID: "c-to", Type: e.Type, Timeout: 3},
		},
	}
	se := newStepExecutionWithIdx(t, tc, f.wsIdx)
	result := se.runSteps()
	for _, ev := range result.Evidence {
		if ev.Action == "ws_receive" && ev.Matched && ev.MatchedType == e.Type {
			return true
		}
	}
	return false
}

// TestRelayCoverage_ProbeTriggerable is spec Phase 1a's live probe: it runs the
// 4-step relay for every declared message_handled edge against a live
// open-agents server and classifies each as client-triggerable vs server-only.
// The classification (logged) is the data Phase 2 uses to narrow the
// requiredEdges denominator, and the precondition for Phase 1b wiring. This
// test runs each case via newStepExecutionWithIdx (NOT the ExecutePlan loop),
// so systemic_failure/target_unreachable escalation does NOT fire — it is safe
// to run all ~60 cases even though server-only ones time out.
func TestRelayCoverage_ProbeTriggerable(t *testing.T) {
	f := setupOpenAgents(t, false)
	edges := readDogfoodVocabEdges(t)

	// Dedup by (From,To,Type) and keep only the message_handled, non-self edges
	// — exactly the set wsRelayCoverageCases / requiredEdges would consider.
	seen := map[string]bool{}
	var triggerable, serverOnly []string
	for _, e := range edges {
		if e.Trigger != "message_handled" || e.FromRole == "" || e.ToRole == "" || e.FromRole == e.ToRole {
			continue
		}
		key := e.FromRole + "|" + e.ToRole + "|" + e.Type
		if seen[key] {
			continue
		}
		seen[key] = true

		label := fmt.Sprintf("%s→%s %s", e.FromRole, e.ToRole, e.Type)
		if probeEdge(t, f, e) {
			triggerable = append(triggerable, label)
		} else {
			serverOnly = append(serverOnly, label)
		}
	}

	sort.Strings(triggerable)
	sort.Strings(serverOnly)
	t.Logf("PROBE SUMMARY: triggerable=%d server-only=%d total=%d",
		len(triggerable), len(serverOnly), len(triggerable)+len(serverOnly))
	t.Logf("TRIGGERABLE:\n  %s", strings.Join(triggerable, "\n  "))
	t.Logf("SERVER-ONLY:\n  %s", strings.Join(serverOnly, "\n  "))

	// Sanity: the device:online peer-join relay is known client-triggerable
	// (the existing TestPathCoverage_LiveOpenAgentsRelay proves it), so at
	// least that must classify as triggerable. We do NOT hard-assert the
	// server-only set — its exact membership is the data this probe gathers.
	require.Contains(t, triggerable, "bridge→web device:online",
		"device:online relay must be classified client-triggerable")
	require.NotEmpty(t, triggerable, "some message_handled types must be client-triggerable")
}

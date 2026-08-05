package examiner

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/types"
)

// TestBuildEvidenceContextIncludesWSMessages verifies that the judge prompt
// carries WebSocket message bodies. WSResult.Summary() intentionally omits
// bodies, so without a dedicated branch the Examiner cannot judge content
// expectations like "payload.approved == true".
func TestBuildEvidenceContextIncludesWSMessages(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "perm round-trip", Target: "ws://x/ws"},
		Status:   agent.StepPassed,
		Result: types.WSResult{
			OK:             true,
			MatchedMessage: `{"type":"permission:response","payload":{"approved":true}}`,
		},
	}
	got := j.buildEvidenceContext(res)
	if !strings.Contains(got, "permission:response") {
		t.Fatalf("evidence missing matched message body:\n%s", got)
	}
	if !strings.Contains(got, "approved") {
		t.Fatalf("evidence missing payload content:\n%s", got)
	}
}

// TestBuildEvidenceContextIncludesMatchAllItems verifies the judge sees EVERY
// item a match_all receive collected, not just the first. WSResult splits a
// match-all burst into MatchedMessage (first) + MatchedMessages (rest) +
// MatchedCount; if buildEvidenceContext renders only MatchedMessage, a 3-item
// batch success shows one item and the judge cannot verify "all items" —
// inviting an uncertain/low-confidence verdict on a real pass.
func TestBuildEvidenceContextIncludesMatchAllItems(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "every event ok", Target: "ws://x/ws"},
		Status:   agent.StepPassed,
		Result: types.WSResult{
			OK:             true,
			MatchedCount:   3,
			MatchedMessage: `{"type":"event","payload":{"id":"a","ok":true}}`,
			MatchedMessages: []string{
				`{"type":"event","payload":{"id":"b","ok":true}}`,
				`{"type":"event","payload":{"id":"c","ok":true}}`,
			},
		},
	}
	got := j.buildEvidenceContext(res)
	if !strings.Contains(got, `"id":"b"`) || !strings.Contains(got, `"id":"c"`) {
		t.Fatalf("evidence missing non-first match-all items:\n%s", got)
	}
	if !strings.Contains(got, "3") {
		t.Fatalf("evidence missing the matched item count:\n%s", got)
	}
}

// TestBuildEvidenceContextIncludesStepTrace verifies that a MULTI-step (Steps /
// ws_flow) case surfaces its per-step evidence to the judge. runSteps sets
// StepResult.Result to the LAST step (often an inert ws_disconnect) and
// StepResult.Evidence to the full step trace. The decisive matched receive
// (e.g. the relayed device:online) lives in Evidence, NOT in Result; if the
// judge prompt only shows Result, a passing relay looks like an empty
// disconnect and gets misjudged (judge drift / near-fail). Reproduces the
// 2026-07-28 dogfood regression where tc-001 matched device:online yet scored
// "zero tool calls" / uncertain 0.2.
func TestBuildEvidenceContextIncludesStepTrace(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{
			Name:        "relay: web receives device:online when bridge connects",
			Target:      "ws://x/ws/user",
			Expectation: "web receives the relayed device:online signal after bridge connects",
		},
		Status: agent.StepPassed,
		// Last step is an inert disconnect — MatchedMessage empty, exactly like
		// tc-001's final ws_disconnect. This is what the judge currently sees.
		Result: types.WSResult{OK: true},
		Evidence: []agent.Evidence{
			{Type: "ws_messages", Content: "ws_connect: ws ok connection_id=web"},
			{Type: "ws_messages", Content: "ws_connect: ws ok connection_id=bridge"},
			{Type: "ws_messages", Content: "ws_receive: ws ok (matched=1 seen=0, 96ms)"},
			{Type: "ws_messages", Content: "ws_disconnect: ws ok"},
		},
	}
	got := j.buildEvidenceContext(res)
	if !strings.Contains(got, "matched=1") {
		t.Fatalf("multi-step evidence missing the decisive matched receive from StepResult.Evidence:\n%s", got)
	}
	if !strings.Contains(got, "ws_connect") {
		t.Fatalf("multi-step evidence missing the step trace:\n%s", got)
	}
}

// TestBuildEvidenceContextIncludesHTTPBody locks the HTTP branch of the
// refactored three-branch switch (HTTP/WS/default). Summary() omits the body,
// so the dedicated branch must surface status code, body, and error text for
// the Examiner to judge content expectations.
func TestBuildEvidenceContextIncludesHTTPBody(t *testing.T) {
	j := &Judge{}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "health check", Target: "http://x/api"},
		Status:   agent.StepPassed,
		Result: types.HTTPResult{
			OK:         true,
			StatusCode: 200,
			Body:       `{"ok":true}`,
			Err:        "",
		},
	}
	got := j.buildEvidenceContext(res)
	if !strings.Contains(got, "200") {
		t.Fatalf("evidence missing status code:\n%s", got)
	}
	if !strings.Contains(got, `"ok":true`) {
		t.Fatalf("evidence missing response body:\n%s", got)
	}

	// With a non-empty Err, the HTTP branch appends an "Error:" line.
	resErr := agent.StepResult{
		TestCase: &agent.TestCase{Name: "health check", Target: "http://x/api"},
		Status:   agent.StepFailed,
		Result: types.HTTPResult{
			OK:         false,
			StatusCode: 500,
			Body:       `{"ok":false}`,
			Err:        "upstream timeout",
		},
	}
	gotErr := j.buildEvidenceContext(resErr)
	if !strings.Contains(gotErr, "Error:") {
		t.Fatalf("evidence missing Error line:\n%s", gotErr)
	}
	if !strings.Contains(gotErr, "upstream timeout") {
		t.Fatalf("evidence missing error text:\n%s", gotErr)
	}
}

// TestBuildJudgePromptIncludesVocab verifies the judge prompt carries the
// routing vocabulary when VocabSummary is set, so verdicts on WS cases anchor
// to concrete legal types instead of expectation prose alone.
func TestBuildJudgePromptIncludesVocab(t *testing.T) {
	j := &Judge{config: ExaminerConfig{
		VocabSummary: "\n\n## WS Routing Vocabulary (realtime, 1 edges)\nbridge->web broadcast_web (1): workflow:task_progress\n",
	}}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "relay", Target: "ws://x/ws", Expectation: "web receives progress"},
		Status:   agent.StepPassed,
	}
	got := j.buildJudgePrompt(res)
	if !strings.Contains(got, "WS Routing Vocabulary") || !strings.Contains(got, "workflow:task_progress") {
		t.Fatalf("judge prompt missing vocab block:\n%s", got)
	}
}

// TestBuildJudgePromptOmitsVocabWhenEmpty verifies the non-WS path is
// byte-identical to today: an empty VocabSummary adds nothing.
func TestBuildJudgePromptOmitsVocabWhenEmpty(t *testing.T) {
	j := &Judge{config: ExaminerConfig{}}
	res := agent.StepResult{
		TestCase: &agent.TestCase{Name: "relay", Target: "ws://x/ws", Expectation: "ok"},
		Status:   agent.StepPassed,
	}
	got := j.buildJudgePrompt(res)
	if strings.Contains(got, "WS Routing Vocabulary") {
		t.Fatalf("non-WS prompt should not mention vocab:\n%s", got)
	}
}

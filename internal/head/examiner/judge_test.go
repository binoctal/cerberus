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

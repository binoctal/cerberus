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

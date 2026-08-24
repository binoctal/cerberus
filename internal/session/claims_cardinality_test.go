package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func cardinalityFixture() (realActorIndex, []agent.StepResult) {
	idx := realActorIndex{
		Roles:     map[string]bool{"bridge": true, "bridge2": true, "bridge3": true},
		RoleActor: map[string]string{"bridge": "b1", "bridge2": "b2", "bridge3": "b3"},
		ActorIDs: map[string][]string{
			"b1": {"dev-1"}, "b2": {"dev-2"}, "b3": {"dev-3"},
		},
		ActorByValue: map[string]string{"dev-1": "b1", "dev-2": "b2", "dev-3": "b3"},
	}
	// One passing real-tier case per replica, bound to the cardinality claim:
	// each body carries the {{role.deviceId}} placeholder for its role.
	mk := func(id, role string) agent.StepResult {
		return agent.StepResult{Status: agent.StepPassed, TestCase: &agent.TestCase{
			ID: id, Claims: []string{"multi"},
			Steps: []agent.TestStep{{Action: "ws_send", Message: `{"deviceId":"{{` + role + `.deviceId}}"`}},
		}}
	}
	return idx, []agent.StepResult{mk("c1", "bridge"), mk("c2", "bridge2"), mk("c3", "bridge3")}
}

func TestCardinalityProvenAtThree(t *testing.T) {
	idx, results := cardinalityFixture()
	claims := []project.Claim{{ID: "multi", Critical: true, ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if len(v) != 1 || v[0].Status != ClaimProven || v[0].Reason != "" {
		t.Fatalf("verdict = %+v, want proven without reason", v)
	}
}

func TestCardinalityShortfallIsEmulatedOnlyWithReason(t *testing.T) {
	idx, results := cardinalityFixture()
	results = results[:2] // only two replicas exercised
	claims := []project.Claim{{ID: "multi", Critical: true, ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimEmulatedOnly || v[0].Reason != "cardinality 2/3" {
		t.Fatalf("verdict = %+v, want emulated-only cardinality 2/3", v[0])
	}
}

func TestCardinalityCountsActorsNotRoles(t *testing.T) {
	// bridge and bridge2 backed by the SAME actor: two roles, one identity —
	// two cases exercising both roles satisfy cardinality 1, not 2.
	idx, results := cardinalityFixture()
	idx.RoleActor["bridge2"] = "b1"
	idx.ActorIDs = map[string][]string{"b1": {"dev-1"}, "b3": {"dev-3"}}
	idx.ActorByValue = map[string]string{"dev-1": "b1", "dev-3": "b3"}
	results = results[:2] // only the two same-actor cases
	claims := []project.Claim{{ID: "multi", ImpliesCardinality: 2}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimEmulatedOnly || v[0].Reason != "cardinality 1/2" {
		t.Fatalf("same-actor two-roles must count once: %+v", v[0])
	}
}

func TestCardinalityRawIdBodyMatchAttributesActor(t *testing.T) {
	idx, results := cardinalityFixture()
	results[2].TestCase.Steps[0].Message = `{"deviceId":"dev-3"}`
	claims := []project.Claim{{ID: "multi", ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimProven {
		t.Fatalf("raw-id match must credit the owning actor: %+v", v[0])
	}
}

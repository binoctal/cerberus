package session

import (
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// ClaimStatus is a claim's verdict after reconciling the ledger against the
// session's final evidence (spec amendment #1 semantics).
type ClaimStatus string

const (
	// ClaimProven: >=1 passing bound case with real-tier evidence.
	ClaimProven ClaimStatus = "proven"
	// ClaimEmulatedOnly: passing bound cases exist, all emulated-tier.
	ClaimEmulatedOnly ClaimStatus = "emulated-only"
	// ClaimUnevidenced: no bound cases, or none passing.
	ClaimUnevidenced ClaimStatus = "unevidenced"
)

// Evidence tiers for a passing case's evidence.
const (
	evidenceReal     = "real"
	evidenceEmulated = "emulated"
)

// ClaimVerdict is one ledger claim reconciled against the session's results.
type ClaimVerdict struct {
	Claim  project.Claim
	Status ClaimStatus
	// Cases lists the bound case ids, passing first (failures still name
	// their binding so a report can show what was attempted).
	Cases []string
}

// caseEvidenceTier reports the best tier a PASSED case's evidence reaches.
// real: the case connects as a real-process role (a step Role or http
// AuthRole bound to a real-process actor), or a raw send body (case Body,
// ws_send Message, http_request Body) contains any value from realActorIds —
// the harness-captured identities (deviceId etc., including the actor's
// path-param values) the caller collected from session state. {{...}}
// resolution is unavailable here, so the match runs against the raw strings.
//
// realRoleActors is keyed by ROLE NAME — the same namespace as
// scout.realProcessRoles (protocol role names whose credential_ref names a
// fidelity: real-process actor). Actor names must NOT be used as keys: step
// Role/AuthRole carry role names, so an actor-keyed map would match nothing
// and every case would silently degrade to the emulated tier.
func caseEvidenceTier(tc agent.TestCase, realRoleActors map[string]bool, realActorIds []string) string {
	for _, s := range tc.Steps {
		// Empty guard mirrors realActorIds: a caller-inserted "" key would
		// otherwise match the empty Role/AuthRole most steps carry.
		if realRoleActor(realRoleActors, s.Role) || realRoleActor(realRoleActors, s.AuthRole) {
			return evidenceReal
		}
	}
	if len(realActorIds) > 0 {
		for _, body := range rawSendBodies(tc) {
			for _, id := range realActorIds {
				if id != "" && strings.Contains(body, id) {
					return evidenceReal
				}
			}
		}
	}
	// Cross-actor placeholder: a send body referencing {{realRole.param}}
	// routes at the real process. The raw string cannot contain the captured
	// id (resolution happens at send time), but the placeholder resolves ONLY
	// from that real actor's captured params — unresolved is a hard error —
	// so a PASSING case necessarily addressed the real process.
	for _, body := range rawSendBodies(tc) {
		for role := range realRoleActors {
			if role != "" && strings.Contains(body, "{{"+role+".") {
				return evidenceReal
			}
		}
	}
	return evidenceEmulated
}

// realRoleActor reports whether the name is a non-empty key of the
// real-process role set.
func realRoleActor(realRoleActors map[string]bool, name string) bool {
	return name != "" && realRoleActors[name]
}

// rawSendBodies gathers every raw request body a case can emit: the legacy
// case-level Body plus each step's ws_send Message and http_request Body.
func rawSendBodies(tc agent.TestCase) []string {
	bodies := make([]string, 0, 1+len(tc.Steps))
	if tc.Body != "" {
		bodies = append(bodies, tc.Body)
	}
	for _, s := range tc.Steps {
		if s.Message != "" {
			bodies = append(bodies, s.Message)
		}
		if s.Body != "" {
			bodies = append(bodies, s.Body)
		}
	}
	return bodies
}

// ReconcileClaims computes every claim's verdict. claims: the ledger;
// results: final step results (Status + TestCase); realRoleActors: role
// names bound to fidelity real-process actors (keyed by ROLE NAME — same
// namespace as scout.realProcessRoles; actor names must NOT be used as
// keys, see caseEvidenceTier); realActorIds: their captured identity values
// present in the session. Repair-inherited bindings (Claims copied onto a
// Replaces/FallbackFor case) prove like any other binding.
func ReconcileClaims(claims []project.Claim, results []agent.StepResult, realRoleActors map[string]bool, realActorIds []string) []ClaimVerdict {
	verdicts := make([]ClaimVerdict, 0, len(claims))
	for _, c := range claims {
		v := ClaimVerdict{Claim: c, Status: ClaimUnevidenced}
		var passing, failing []string
		for _, r := range results {
			if r.TestCase == nil || !claimsBound(r.TestCase.Claims, c.ID) {
				continue
			}
			if r.Status == agent.StepPassed {
				passing = append(passing, r.TestCase.ID)
				if caseEvidenceTier(*r.TestCase, realRoleActors, realActorIds) == evidenceReal {
					v.Status = ClaimProven
				}
			} else {
				failing = append(failing, r.TestCase.ID)
			}
		}
		// Passing cases exist but none reached the real tier.
		if v.Status == ClaimUnevidenced && len(passing) > 0 {
			v.Status = ClaimEmulatedOnly
		}
		v.Cases = append(passing, failing...)
		verdicts = append(verdicts, v)
	}
	return verdicts
}

// claimsBound reports whether the case binds the claim id.
func claimsBound(bound []string, claimID string) bool {
	for _, id := range bound {
		if id == claimID {
			return true
		}
	}
	return false
}

// ClaimsGateFailed reports whether the hard gate bites: any critical claim
// without a WontTest exemption whose status is not proven. Such a session is
// incomplete (exit 3) even when execution itself succeeded.
func ClaimsGateFailed(verdicts []ClaimVerdict) bool {
	for _, v := range verdicts {
		if v.Claim.Critical && !v.Claim.WontTest() && v.Status != ClaimProven {
			return true
		}
	}
	return false
}

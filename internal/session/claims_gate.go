package session

import (
	"errors"
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// ErrClaimsGate is the sentinel returned as a session's final error when the
// claims gate bites: at least one critical claim is not proven and carries no
// wont-test exemption. The session is incomplete rather than failed —
// cerberus run exits 3 on it, distinct from execution failure.
var ErrClaimsGate = errors.New("claims gate: critical claims not proven")

// realActorIndex is the identity context ReconcileClaims matches evidence
// against, collected once per session.
type realActorIndex struct {
	// Roles: protocol role names bound to a real-process actor (today's
	// realRoleActors namespace — step Role/AuthRole carry role names; actor
	// names must NOT be keys, they never match step Role/AuthRole).
	Roles map[string]bool
	// RoleActor: role name -> backing actor name (cardinality attribution).
	RoleActor map[string]string
	// ActorIDs: actor name -> captured path-param values (deviceId etc.).
	ActorIDs map[string][]string
	// ActorByValue: captured value -> actor name (raw-id body matches credit
	// the owning actor).
	ActorByValue map[string]string
}

// flatIDs returns every captured identity value (caseEvidenceTier's input).
func (idx realActorIndex) flatIDs() []string {
	var out []string
	for _, ids := range idx.ActorIDs {
		out = append(out, ids...)
	}
	return out
}

// collectRealIdentities walks the config for fidelity real-process actors and
// builds the identity index: roles keyed by ROLE NAME (the same namespace as
// scout.realProcessRoles), the role->actor map, and each actor's captured
// path-param values (deviceId etc.).
func collectRealIdentities(cfg *project.Config) realActorIndex {
	if cfg == nil {
		return realActorIndex{}
	}
	realActors := map[string]bool{}
	idx := realActorIndex{
		RoleActor:    map[string]string{},
		ActorIDs:     map[string][]string{},
		ActorByValue: map[string]string{},
	}
	for _, a := range cfg.Actors {
		if a.Fidelity != project.FidelityRealProcess {
			continue
		}
		realActors[a.Name] = true
		for _, v := range a.Credentials.PathParams {
			idx.ActorIDs[a.Name] = append(idx.ActorIDs[a.Name], v)
			if v != "" {
				idx.ActorByValue[v] = a.Name
			}
		}
	}
	if len(realActors) == 0 {
		return realActorIndex{}
	}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		for name, r := range svc.Protocol.Roles {
			if r != nil && r.CredentialRef != "" && realActors[r.CredentialRef] {
				if idx.Roles == nil {
					idx.Roles = map[string]bool{}
				}
				idx.Roles[name] = true
				idx.RoleActor[name] = r.CredentialRef
			}
		}
	}
	return idx
}

// reconcileClaimsInto reconciles the config's claims ledger against the
// session's final results and folds the verdicts into the summary: the three
// status counts, the red lines (critical claims failing the gate, one entry
// each) and the gate flag Session.Run/Resume turn into ErrClaimsGate. A no-op
// when the project has no ledger.
func reconcileClaimsInto(summary *SessionSummary, cfg *project.Config, results []agent.StepResult) {
	if summary == nil || cfg == nil || cfg.Claims == nil || len(cfg.Claims.Claims) == 0 {
		return
	}
	realRoleActors := collectRealIdentities(cfg)
	verdicts := ReconcileClaims(cfg.Claims.Claims, results, realRoleActors)
	for _, v := range verdicts {
		switch v.Status {
		case ClaimProven:
			summary.ClaimsProven++
		case ClaimEmulatedOnly:
			summary.ClaimsEmulatedOnly++
		case ClaimUnevidenced:
			// A wont-test exemption is a deliberate opt-out (evidence lives in
			// another suite), not an open coverage gap — count it separately so
			// "N unevidenced" stays actionable.
			if v.Claim.WontTest() {
				summary.ClaimsWontTest++
			} else {
				summary.ClaimsUnevidenced++
			}
		}
	}
	summary.ClaimsVerdicts = verdicts
	if !ClaimsGateFailed(verdicts) {
		return
	}
	summary.ClaimsGateTriggered = true
	for _, v := range verdicts {
		if v.Claim.Critical && !v.Claim.WontTest() && v.Status != ClaimProven {
			line := fmt.Sprintf("%s — %s (%s)", v.Claim.ID, v.Claim.Text, v.Status)
			if v.Reason != "" {
				line += fmt.Sprintf(" (reason: %s)", v.Reason)
			}
			summary.ClaimsRedLines = append(summary.ClaimsRedLines, line)
		}
	}
}

// gateErrorIfFailed returns ErrClaimsGate when the summary's claims gate bit,
// nil otherwise. Session.Run/Resume call it as their final step so the gate
// surfaces after finalize has persisted the summary.
func gateErrorIfFailed(summary *SessionSummary) error {
	if summary != nil && summary.ClaimsGateTriggered {
		return ErrClaimsGate
	}
	return nil
}

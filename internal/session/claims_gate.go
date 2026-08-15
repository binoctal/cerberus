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

// collectRealIdentities walks the config for fidelity real-process actors and
// returns the two identity sets ReconcileClaims matches evidence against:
// realRoleActors keyed by ROLE NAME (protocol roles whose credential_ref names
// a real-process actor — the same namespace as scout.realProcessRoles; actor
// names must NOT be keys, they never match step Role/AuthRole), and
// realActorIds gathering every captured path-param value (deviceId etc.) from
// those actors' credentials.
func collectRealIdentities(cfg *project.Config) (realRoleActors map[string]bool, realActorIds []string) {
	if cfg == nil {
		return nil, nil
	}
	realActors := map[string]bool{}
	for _, a := range cfg.Actors {
		if a.Fidelity != project.FidelityRealProcess {
			continue
		}
		realActors[a.Name] = true
		for _, v := range a.Credentials.PathParams {
			realActorIds = append(realActorIds, v)
		}
	}
	if len(realActors) == 0 {
		return nil, nil
	}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		for name, r := range svc.Protocol.Roles {
			if r != nil && r.CredentialRef != "" && realActors[r.CredentialRef] {
				if realRoleActors == nil {
					realRoleActors = map[string]bool{}
				}
				realRoleActors[name] = true
			}
		}
	}
	return realRoleActors, realActorIds
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
	realRoleActors, realActorIds := collectRealIdentities(cfg)
	verdicts := ReconcileClaims(cfg.Claims.Claims, results, realRoleActors, realActorIds)
	for _, v := range verdicts {
		switch v.Status {
		case ClaimProven:
			summary.ClaimsProven++
		case ClaimEmulatedOnly:
			summary.ClaimsEmulatedOnly++
		case ClaimUnevidenced:
			summary.ClaimsUnevidenced++
		}
	}
	summary.ClaimsVerdicts = verdicts
	if !ClaimsGateFailed(verdicts) {
		return
	}
	summary.ClaimsGateTriggered = true
	for _, v := range verdicts {
		if v.Claim.Critical && !v.Claim.WontTest() && v.Status != ClaimProven {
			summary.ClaimsRedLines = append(summary.ClaimsRedLines,
				fmt.Sprintf("%s — %s (%s)", v.Claim.ID, v.Claim.Text, v.Status))
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

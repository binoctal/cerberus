package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// realRoleFixture is the minimal shape shared by the reconciliation matrix:
// a "device" role bound to a real-process actor whose captured identity is
// device_x.
func realRoleFixture() (map[string]bool, []string) {
	return map[string]bool{"device": true}, []string{"device_x"}
}

// realRoleIndexFixture is the same one-role world as realRoleFixture, in the
// index shape ReconcileClaims consumes.
func realRoleIndexFixture() realActorIndex {
	return realActorIndex{
		Roles:        map[string]bool{"device": true},
		RoleActor:    map[string]string{"device": "device-actor"},
		ActorIDs:     map[string][]string{"device-actor": {"device_x"}},
		ActorByValue: map[string]string{"device_x": "device-actor"},
	}
}

func passingResult(tc agent.TestCase) agent.StepResult {
	return agent.StepResult{TestCase: &tc, Status: agent.StepPassed}
}

// TestCaseEvidenceTier pins the amendment-#1 tier semantics for a single
// passed case: real via real-process role connection, real via a raw send
// body referencing a captured identity, emulated otherwise.
func TestCaseEvidenceTier(t *testing.T) {
	realRoles, realIds := realRoleFixture()
	t.Run("connects as real-process role", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-001", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "device"},
		}}
		assert.Equal(t, "real", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("http auth as real-process role", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-002", Steps: []agent.TestStep{
			{Action: "http_request", AuthRole: "device"},
		}}
		assert.Equal(t, "real", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("emulated body references captured identity", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-003", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"deviceId":"device_x","type":"cmd"}`},
		}}
		assert.Equal(t, "real", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("emulated case body references captured identity", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-004", Body: `{"deviceId":"device_x"}`}
		assert.Equal(t, "real", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("emulated with no reference", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-005", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"deviceId":"emulated-1"}`},
		}}
		assert.Equal(t, "emulated", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("emulated body routes at a real role via cross-actor placeholder", func(t *testing.T) {
		// realE2E L1 shape: the emulated side sends {{device.deviceId}} — the
		// raw string cannot contain the captured id, but the placeholder
		// resolves ONLY from the real actor's captured params at send time
		// (unresolved is a hard error), so a passing case addressed the real
		// process.
		tc := agent.TestCase{ID: "tc-007", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"type":"session:start","payload":{"deviceId":"{{device.deviceId}}"}}`},
		}}
		assert.Equal(t, "real", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("placeholder of a non-real role stays emulated", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-008", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
			{Action: "ws_send", Message: `{"deviceId":"{{peer.deviceId}}"}`},
		}}
		assert.Equal(t, "emulated", caseEvidenceTier(tc, realRoles, realIds))
	})
	t.Run("no real-process actors at all", func(t *testing.T) {
		tc := agent.TestCase{ID: "tc-006", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "web"},
		}}
		assert.Equal(t, "emulated", caseEvidenceTier(tc, nil, nil))
	})
	// CONTRACT LOCK: realRoleActors is keyed by ROLE name (the namespace of
	// scout.realProcessRoles — role names derived from credential_ref), NOT
	// by actor name. Step Role/AuthRole carry role names; an actor-keyed map
	// would match nothing and silently degrade every case to emulated.
	t.Run("keyed by role name, not actor name", func(t *testing.T) {
		// Actor "openagents-cli" (fidelity real-process) occupies role "device".
		tc := agent.TestCase{ID: "tc-007", Steps: []agent.TestStep{
			{Action: "ws_connect", Role: "device"},
		}}
		assert.Equal(t, "real", caseEvidenceTier(tc, map[string]bool{"device": true}, realIds))
		assert.Equal(t, "emulated", caseEvidenceTier(tc, map[string]bool{"openagents-cli": true}, realIds),
			"actor-name keys must not match step roles")
	})
	t.Run("empty key does not mark empty roles real", func(t *testing.T) {
		// Most steps carry an empty Role/AuthRole; a caller-inserted "" key
		// would otherwise flip every case to the real tier.
		tc := agent.TestCase{ID: "tc-008", Steps: []agent.TestStep{
			{Action: "http_request"},
		}}
		assert.Equal(t, "emulated", caseEvidenceTier(tc, map[string]bool{"": true}, realIds))
	})
}

// TestReconcileClaims covers the full claim-status matrix: proven (both real
// tier paths), emulated-only, unevidenced (unbound / all failed), case-id
// ordering (passing first), and repair-inherited bindings counting as
// evidence.
func TestReconcileClaims(t *testing.T) {
	idx := realRoleIndexFixture()

	t.Run("passing case connecting as real role is proven", func(t *testing.T) {
		claims := []project.Claim{{ID: "schedule-real-cli", Text: "调度真实 AI CLI 执行任务"}}
		results := []agent.StepResult{passingResult(agent.TestCase{
			ID: "tc-001", Claims: []string{"schedule-real-cli"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}},
		})}
		v := ReconcileClaims(claims, results, idx)
		assert.Len(t, v, 1)
		assert.Equal(t, ClaimProven, v[0].Status)
		assert.Equal(t, []string{"tc-001"}, v[0].Cases)
	})

	t.Run("passing emulated case with captured identity in body is proven", func(t *testing.T) {
		claims := []project.Claim{{ID: "multi-device", Text: "支持多设备"}}
		results := []agent.StepResult{passingResult(agent.TestCase{
			ID: "tc-002", Claims: []string{"multi-device"},
			Steps: []agent.TestStep{
				{Action: "ws_connect", Role: "web"},
				{Action: "ws_send", Message: `{"deviceId":"device_x","type":"cmd"}`},
			},
		})}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimProven, v[0].Status)
	})

	t.Run("passing emulated case without reference is emulated-only", func(t *testing.T) {
		claims := []project.Claim{{ID: "ws-relay", Text: "消息中继"}}
		results := []agent.StepResult{passingResult(agent.TestCase{
			ID: "tc-003", Claims: []string{"ws-relay"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "web"}},
		})}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimEmulatedOnly, v[0].Status)
	})

	t.Run("no bound cases is unevidenced", func(t *testing.T) {
		claims := []project.Claim{{ID: "permission-approval", Text: "权限审批"}}
		results := []agent.StepResult{passingResult(agent.TestCase{
			ID: "tc-004", Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}},
		})}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimUnevidenced, v[0].Status)
		assert.Empty(t, v[0].Cases)
	})

	t.Run("bound but failed is unevidenced with the case listed", func(t *testing.T) {
		claims := []project.Claim{{ID: "mission-planning", Text: "任务规划"}}
		results := []agent.StepResult{{TestCase: &agent.TestCase{
			ID: "tc-005", Claims: []string{"mission-planning"},
		}, Status: agent.StepFailed}}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimUnevidenced, v[0].Status)
		assert.Equal(t, []string{"tc-005"}, v[0].Cases)
	})

	t.Run("case ids list passing first", func(t *testing.T) {
		claims := []project.Claim{{ID: "mixed", Text: "混合绑定"}}
		results := []agent.StepResult{
			{TestCase: &agent.TestCase{ID: "tc-fail", Claims: []string{"mixed"}}, Status: agent.StepFailed},
			passingResult(agent.TestCase{ID: "tc-pass", Claims: []string{"mixed"}}),
		}
		v := ReconcileClaims(claims, results, realActorIndex{})
		assert.Equal(t, ClaimEmulatedOnly, v[0].Status)
		assert.Equal(t, []string{"tc-pass", "tc-fail"}, v[0].Cases)
	})

	t.Run("repair-inherited case proves the claim", func(t *testing.T) {
		claims := []project.Claim{{ID: "schedule-real-cli", Text: "调度真实 AI CLI 执行任务", Critical: true}}
		results := []agent.StepResult{passingResult(agent.TestCase{
			// Replacement case: Claims were inherited from the failed original
			// via the Replaces path (inheritClaims); it proves like any other
			// binding — no special casing.
			ID: "tc-006-r", Replaces: "tc-006", Claims: []string{"schedule-real-cli"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}},
		})}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimProven, v[0].Status)
		assert.False(t, ClaimsGateFailed(v))
	})

	t.Run("one real-tier passing case among failures is proven", func(t *testing.T) {
		claims := []project.Claim{{ID: "mixed-tier", Text: "任一实跑即可"}}
		results := []agent.StepResult{
			{TestCase: &agent.TestCase{ID: "tc-emu", Claims: []string{"mixed-tier"},
				Steps: []agent.TestStep{{Action: "ws_connect", Role: "web"}}}, Status: agent.StepPassed},
			passingResult(agent.TestCase{ID: "tc-real", Claims: []string{"mixed-tier"},
				Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}}}),
		}
		v := ReconcileClaims(claims, results, idx)
		assert.Equal(t, ClaimProven, v[0].Status)
		assert.Equal(t, []string{"tc-emu", "tc-real"}, v[0].Cases)
	})
}

// TestClaimsGateFailed pins the hard gate: only critical, non-exempt claims
// that are not proven fail the gate.
func TestClaimsGateFailed(t *testing.T) {
	t.Run("critical emulated-only fails the gate", func(t *testing.T) {
		v := []ClaimVerdict{{Claim: project.Claim{ID: "c", Text: "t", Critical: true}, Status: ClaimEmulatedOnly}}
		assert.True(t, ClaimsGateFailed(v))
	})
	t.Run("critical unevidenced fails the gate", func(t *testing.T) {
		v := []ClaimVerdict{{Claim: project.Claim{ID: "c", Text: "t", Critical: true}, Status: ClaimUnevidenced}}
		assert.True(t, ClaimsGateFailed(v))
	})
	t.Run("critical wont-test passes the gate", func(t *testing.T) {
		v := []ClaimVerdict{{Claim: project.Claim{ID: "c", Text: "t", Critical: true,
			StatusAnnotation: "wont-test(no real bridge in this environment)"}, Status: ClaimUnevidenced}}
		assert.False(t, ClaimsGateFailed(v))
	})
	t.Run("non-critical unevidenced passes the gate", func(t *testing.T) {
		v := []ClaimVerdict{{Claim: project.Claim{ID: "c", Text: "t"}, Status: ClaimUnevidenced}}
		assert.False(t, ClaimsGateFailed(v))
	})
	t.Run("critical proven passes the gate", func(t *testing.T) {
		v := []ClaimVerdict{{Claim: project.Claim{ID: "c", Text: "t", Critical: true}, Status: ClaimProven}}
		assert.False(t, ClaimsGateFailed(v))
	})
}

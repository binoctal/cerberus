package session

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// claimsGateFixture builds a config mirroring the live dogfood shape: a
// real-process actor (cli) with captured path params occupying the "device"
// role, an emulated actor on "web".
func claimsGateFixture() *project.Config {
	return &project.Config{
		Actors: []project.Actor{
			{Name: "web", Fidelity: project.FidelityEmulated},
			{Name: "cli", Fidelity: project.FidelityRealProcess, Credentials: project.CredentialRef{
				PathParams: map[string]string{"deviceId": "device_x", "clientId": "c-42"},
			}},
		},
		Services: []project.Service{{
			Name: "relay",
			Protocol: &project.Protocol{
				Roles: map[string]*project.ProtocolRole{
					"web":    {CredentialRef: "web"},
					"device": {CredentialRef: "cli"},
				},
			},
		}},
	}
}

// TestCollectRealIdentities pins the interface contract: realRoleActors is
// keyed by ROLE NAME (scout.realProcessRoles namespace), never by actor name,
// and every captured path-param value of a real-process actor lands in
// realActorIds.
func TestCollectRealIdentities(t *testing.T) {
	roles, ids := collectRealIdentities(claimsGateFixture())
	assert.Equal(t, map[string]bool{"device": true}, roles,
		"role-name keys only; actor-name keys would silently degrade tiers")
	assert.ElementsMatch(t, []string{"device_x", "c-42"}, ids)

	t.Run("no real-process actors", func(t *testing.T) {
		cfg := claimsGateFixture()
		cfg.Actors[1].Fidelity = project.FidelityEmulated
		roles, ids := collectRealIdentities(cfg)
		assert.Empty(t, roles)
		assert.Empty(t, ids)
	})

	t.Run("nil config", func(t *testing.T) {
		roles, ids := collectRealIdentities(nil)
		assert.Nil(t, roles)
		assert.Nil(t, ids)
	})
}

// TestGateErrorIfFailed: the summary gate flag maps to the sentinel, and
// nothing else does.
func TestGateErrorIfFailed(t *testing.T) {
	err := gateErrorIfFailed(&SessionSummary{ClaimsGateTriggered: true})
	assert.ErrorIs(t, err, ErrClaimsGate)

	assert.NoError(t, gateErrorIfFailed(&SessionSummary{}))
	assert.NoError(t, gateErrorIfFailed(nil))
}

// TestReconcileClaimsInto covers the summary wiring: counts per status, red
// lines only for critical claims failing the gate, verdicts stashed, and the
// no-ledger no-op.
func TestReconcileClaimsInto(t *testing.T) {
	cfg := claimsGateFixture()
	cfg.Claims = &project.ClaimsFile{Claims: []project.Claim{
		{ID: "schedule-real-cli", Text: "调度真实 AI CLI 执行任务", Critical: true},
		{ID: "multi-device", Text: "支持多设备", Critical: true},
		{ID: "permission-approval", Text: "权限审批"}, // non-critical
	}}
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-001", Claims: []string{"schedule-real-cli"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}}}, Status: agent.StepPassed},
		{TestCase: &agent.TestCase{ID: "tc-002", Claims: []string{"multi-device"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "web"}}}, Status: agent.StepPassed},
	}

	s := &SessionSummary{}
	reconcileClaimsInto(s, cfg, results)

	assert.Equal(t, 1, s.ClaimsProven)
	assert.Equal(t, 1, s.ClaimsEmulatedOnly)
	assert.Equal(t, 1, s.ClaimsUnevidenced)
	assert.True(t, s.ClaimsGateTriggered)
	assert.Equal(t, []string{"multi-device — 支持多设备 (emulated-only)"}, s.ClaimsRedLines,
		"red lines list exactly the critical claims failing the gate")
	assert.Len(t, s.ClaimsVerdicts, 3)

	t.Run("wont-test exempts from the gate and the red lines", func(t *testing.T) {
		cfg := claimsGateFixture()
		cfg.Claims = &project.ClaimsFile{Claims: []project.Claim{
			{ID: "multi-device", Text: "支持多设备", Critical: true,
				StatusAnnotation: "wont-test(no second bridge in this environment)"},
		}}
		s := &SessionSummary{}
		reconcileClaimsInto(s, cfg, nil)
		assert.False(t, s.ClaimsGateTriggered)
		assert.Empty(t, s.ClaimsRedLines)
		// The exemption is counted as wont-test, not unevidenced — it is a
		// deliberate opt-out, and "N unevidenced" must stay actionable.
		assert.Equal(t, 1, s.ClaimsWontTest)
		assert.Zero(t, s.ClaimsUnevidenced)
		assert.Contains(t, s.claimsLine(), "/ 1 wont-test")
	})

	t.Run("no ledger is a no-op", func(t *testing.T) {
		s := &SessionSummary{}
		reconcileClaimsInto(s, claimsGateFixture(), nil)
		assert.False(t, s.ClaimsGateTriggered)
		assert.Empty(t, s.ClaimsRedLines)
		assert.Empty(t, s.ClaimsVerdicts)
	})
}

// TestReconcileClaimsInto_RealTierViaRole ensures the wiring passes the
// collected identities through: the same result flips proven/emulated-only
// depending on the collected role set.
func TestReconcileClaimsInto_RealTierViaRole(t *testing.T) {
	cfg := claimsGateFixture()
	cfg.Claims = &project.ClaimsFile{Claims: []project.Claim{{ID: "c", Text: "t"}}}
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "tc-1", Claims: []string{"c"},
			Steps: []agent.TestStep{{Action: "ws_connect", Role: "device"}}}, Status: agent.StepPassed},
	}
	s := &SessionSummary{}
	reconcileClaimsInto(s, cfg, results)
	assert.Equal(t, 1, s.ClaimsProven, "role-keyed identities mark the device-role case real")
	assert.False(t, s.ClaimsGateTriggered)
	assert.NoError(t, gateErrorIfFailed(s))
}

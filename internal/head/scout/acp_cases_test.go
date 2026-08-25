package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// acpFixture: one service, web client role, one real bridge role declaring
// acp_cli (fake layer) and a second real role declaring acp_cli + acp_real
// (real layer).
func acpFixture() project.Service {
	return project.Service{Name: "rt", URL: "ws://localhost:8989/ws", Protocol: &project.Protocol{
		TypePath: "type",
		Auth:     &project.ProtocolAuth{CredentialRef: "web"},
		Roles: map[string]*project.ProtocolRole{
			"web": {CredentialRef: "web"},
			"bridge": {CredentialRef: "b1", ACPCli: "claude",
				RequestPayload: map[string]map[string]string{}},
			"bridge-acp": {CredentialRef: "b2", ACPCli: "claude", ACPReal: true},
		},
	}}
}

func TestACPE2ECases_FakeLayer(t *testing.T) {
	cases := acpE2ECases(acpFixture(), map[string]bool{"bridge": true, "bridge-acp": true})
	var fake, real int
	for _, c := range cases {
		switch {
		case strings.Contains(c.ID, "bridge-acpe2e-session"):
			fake++
			if !strings.Contains(c.Steps[1].Message, `"cliType":"claude"`) {
				t.Fatalf("fake case must start the ACP cliType: %s", c.Steps[1].Message)
			}
			if !strings.Contains(c.Steps[1].Message, `"{{bridge.deviceId}}"`) {
				t.Fatalf("fake case must route at the real device: %s", c.Steps[1].Message)
			}
			if !strings.Contains(c.Steps[3].Message, "CERBERUS_ACP_OK") {
				t.Fatalf("fake case prompt must request the marker: %s", c.Steps[3].Message)
			}
		case strings.Contains(c.ID, "bridge-acp-acpreal-session"):
			real++
		}
	}
	if fake != 1 || real != 1 {
		t.Fatalf("want 1 fake + 1 real case, got fake=%d real=%d (%v)", fake, real, caseIDs(cases))
	}
	// Claims follow the role-claims union (relay claim present).
	for _, c := range cases {
		if !containsStr(strings.Join(c.Claims, ","), "ws-relay-messaging") {
			t.Fatalf("case %s claims = %v", c.ID, c.Claims)
		}
	}
}

func TestACPE2ECases_RealLayerShape(t *testing.T) {
	cases := acpE2ECases(acpFixture(), map[string]bool{"bridge": true, "bridge-acp": true})
	for _, c := range cases {
		if !strings.Contains(c.ID, "acpreal-session") {
			continue
		}
		// The real leg waits far longer per receive (real LLM latency).
		maxTimeout := 0
		for _, s := range c.Steps {
			if s.Action == "ws_receive" && s.Timeout > maxTimeout {
				maxTimeout = s.Timeout
			}
		}
		if maxTimeout < 120 {
			t.Fatalf("real case needs >=120s receive windows, max=%d", maxTimeout)
		}
		if !strings.Contains(c.Expectation, "REAL AI agent") {
			t.Fatalf("real case expectation must be LLM-tolerant: %s", c.Expectation)
		}
	}
}

func TestACPE2ECases_GatedOnRoleDeclaration(t *testing.T) {
	svc := acpFixture()
	svc.Protocol.Roles["bridge"].ACPCli = ""
	cases := acpE2ECases(svc, map[string]bool{"bridge": true, "bridge-acp": true})
	for _, c := range cases {
		if strings.Contains(c.ID, "bridge-acpe2e") && !strings.Contains(c.ID, "bridge-acp-") {
			t.Fatalf("role without acp_cli must not emit: %s", c.ID)
		}
	}
	if got := acpE2ECases(acpFixture(), nil); got != nil {
		t.Fatalf("no real roles -> no cases, got %v", caseIDs(got))
	}
}

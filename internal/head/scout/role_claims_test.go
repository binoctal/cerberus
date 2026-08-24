package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRoleClaimsUnion(t *testing.T) {
	// Order-stable dedup against the hardcoded relay claim.
	got := roleClaimBindings(&project.ProtocolRole{Claims: []string{"multi-device-orchestration"}})
	want := []string{"ws-relay-messaging", "multi-device-orchestration"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	dup := roleClaimBindings(&project.ProtocolRole{Claims: []string{"ws-relay-messaging", "x"}})
	if len(dup) != 2 || dup[0] != "ws-relay-messaging" || dup[1] != "x" {
		t.Fatalf("dedup failed: %v", dup)
	}
	if got := roleClaimBindings(nil); len(got) != 1 || got[0] != "ws-relay-messaging" {
		t.Fatalf("nil role must keep the relay claim only: %v", got)
	}
}

// A role declaring claims gets them unioned onto its per-role generated
// cases (reale2e + realresp), alongside the default relay claim.
func TestRealE2ECasesCarryRoleClaims(t *testing.T) {
	cfg := realE2EFixture()
	cfg.Services[0].Protocol.Roles["bridge"].Claims = []string{"multi-device-orchestration"}
	cases := WSCases(cfg, "")
	found := false
	for _, c := range cases {
		if c.ID != "ws-rt-bridge-reale2e-session" && !containsStr(c.ID, "ws-rt-bridge-realresp") {
			continue
		}
		found = true
		if !claimBound(c.Claims, "ws-relay-messaging") || !claimBound(c.Claims, "multi-device-orchestration") {
			t.Fatalf("case %s claims = %v", c.ID, c.Claims)
		}
	}
	if !found {
		t.Fatal("expected reale2e/realresp cases for role bridge")
	}
}

// Roles without declared claims bind exactly as before (regression lock for
// the union change).
func TestRealE2ECasesUnchangedWithoutRoleClaims(t *testing.T) {
	for _, c := range WSCases(realE2EFixture(), "") {
		if containsStr(c.ID, "reale2e") || containsStr(c.ID, "realresp") {
			if len(c.Claims) != 1 || c.Claims[0] != "ws-relay-messaging" {
				t.Fatalf("case %s claims = %v, want [ws-relay-messaging]", c.ID, c.Claims)
			}
		}
	}
}

func claimBound(claims []string, id string) bool {
	for _, v := range claims {
		if v == id {
			return true
		}
	}
	return false
}

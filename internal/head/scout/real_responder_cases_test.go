package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// TestRealResponderCases_DeclarativeExchanges: each responses entry of a real
// role yields connect → send (deviceId-routed, request_payload defaults) →
// receive-reply. Only the emulated client connects.
func TestRealResponderCases_DeclarativeExchanges(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "ws://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"web": {CredentialRef: "web-actor"},
			"bridge": {CredentialRef: "bridge-pty-1",
				Responses: map[string]string{
					"prompts:sync": "prompts:synced",
					"config:sync":  "config:synced",
				},
				RequestPayload: map[string]map[string]string{
					"config:sync": {"version": "9"},
				}},
		}},
	}
	cases := realResponderCases(svc, map[string]bool{"bridge": true})
	if len(cases) != 2 {
		t.Fatalf("cases = %d, want 2: %+v", len(cases), cases)
	}
	// sorted keys: config:sync before prompts:sync
	c := cases[0]
	if c.Steps[1].Message == "" || !containsStr(c.Steps[1].Message, `"type":"config:sync"`) {
		t.Errorf("send body = %q", c.Steps[1].Message)
	}
	if !containsStr(c.Steps[1].Message, `"deviceId":"{{bridge.deviceId}}"`) {
		t.Errorf("deviceId routing placeholder missing: %q", c.Steps[1].Message)
	}
	if !containsStr(c.Steps[1].Message, `"version":9`) {
		t.Errorf("request_payload default missing (raw JSON number): %q", c.Steps[1].Message)
	}
	if c.Steps[2].Type != "config:synced" {
		t.Errorf("reply receive = %q", c.Steps[2].Type)
	}
	if c.Steps[0].Role != "web" {
		t.Errorf("only the emulated client connects: %+v", c.Steps[0])
	}
	if len(c.Claims) != 1 || c.Claims[0] != "ws-relay-messaging" {
		t.Errorf("claims = %v", c.Claims)
	}
}

// TestRealResponderCases_NoRealRole: without a real role nothing emits.
func TestRealResponderCases_NoRealRole(t *testing.T) {
	svc := project.Service{Name: "x", Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
		"web": {CredentialRef: "w", Responses: map[string]string{"a": "b"}},
	}}}
	if got := realResponderCases(svc, nil); got != nil {
		t.Errorf("no real role emitted %d cases", len(got))
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestRealResponderCases_SkipsHTTPOnlyClient: an http_only role (credential
// carrier for AuthRole injection, never WS-connects) must not be picked as
// the emulated client even when its name sorts first.
func TestRealResponderCases_SkipsHTTPOnlyClient(t *testing.T) {
	svc := project.Service{
		Name: "realtime",
		URL:  "ws://localhost:8989/ws/{userId}",
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{
			"admin":  {CredentialRef: "admin-actor", HTTPOnly: true},
			"web":    {CredentialRef: "web-actor"},
			"bridge": {CredentialRef: "bridge-pty-1", Responses: map[string]string{"config:sync": "config:synced"}},
		}},
	}
	cases := realResponderCases(svc, map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(cases))
	}
	if cases[0].Steps[0].Role != "web" {
		t.Errorf("client = %q, want web (http_only admin skipped)", cases[0].Steps[0].Role)
	}
}

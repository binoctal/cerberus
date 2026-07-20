package project

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestProtocolYAMLRoundTrip(t *testing.T) {
	in := `
name: rt
url: http://localhost:8787
protocol:
  framing: json
  type_path: data.event
  auth:
    strategy: query
    param: token
    credential_ref: web-actor
`
	var svc Service
	if err := yaml.Unmarshal([]byte(in), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol == nil {
		t.Fatal("protocol is nil")
	}
	if svc.Protocol.Framing != "json" || svc.Protocol.TypePath != "data.event" {
		t.Fatalf("framing/type_path = %q/%q", svc.Protocol.Framing, svc.Protocol.TypePath)
	}
	if svc.Protocol.Auth == nil ||
		svc.Protocol.Auth.Strategy != "query" ||
		svc.Protocol.Auth.Param != "token" ||
		svc.Protocol.Auth.CredentialRef != "web-actor" {
		t.Fatalf("auth = %+v", svc.Protocol.Auth)
	}
}

func TestServiceWithoutProtocol(t *testing.T) {
	var svc Service
	if err := yaml.Unmarshal([]byte("name: x\nurl: http://x\n"), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol != nil {
		t.Fatalf("protocol should be nil when absent, got %+v", svc.Protocol)
	}
}

func TestProtocolRolesYAMLRoundTrip(t *testing.T) {
	in := `
name: rt
url: http://localhost:8787
protocol:
  type_path: type
  auth: { strategy: query, param: token, credential_ref: web-actor }
  roles:
    web:
      credential_ref: web-actor
      params: { type: web }
      handshake: { await_type: devices:sync, timeout: 5 }
    bridge:
      credential_ref: bridge-actor
      params: { type: bridge }
`
	var svc Service
	if err := yaml.Unmarshal([]byte(in), &svc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if svc.Protocol == nil || len(svc.Protocol.Roles) != 2 {
		t.Fatalf("roles = %+v", svc.Protocol)
	}
	web := svc.Protocol.Roles["web"]
	if web == nil || web.CredentialRef != "web-actor" || web.Params["type"] != "web" {
		t.Fatalf("web role = %+v", web)
	}
	if web.Handshake == nil || web.Handshake.AwaitType != "devices:sync" || web.Handshake.Timeout != 5 {
		t.Fatalf("web handshake = %+v", web.Handshake)
	}
	bridge := svc.Protocol.Roles["bridge"]
	if bridge == nil || bridge.Handshake != nil {
		t.Fatalf("bridge role = %+v", bridge)
	}
}

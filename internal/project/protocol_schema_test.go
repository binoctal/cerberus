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

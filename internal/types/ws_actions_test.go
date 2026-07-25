package types

import (
	"encoding/json"
	"testing"
)

// TestWSReceiveActionRoundTrip verifies the marshal half of the round-trip
// (UnmarshalAction was deleted in S3 — Agent now emits typed tool calls). We
// unmarshal envelope.Raw directly into the concrete type to confirm the
// fields survive the JSON shape Examiner's judge prompt consumes.
func TestWSReceiveActionRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSReceiveAction{
		ConnectionID: "conn-1",
		Type:         "permission:response",
		Timeout:      5,
		Decisive:     true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if envelope.Type != ActionWSReceive {
		t.Fatalf("type = %s, want %s", envelope.Type, ActionWSReceive)
	}
	var r WSReceiveAction
	if err := json.Unmarshal(envelope.Raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ConnectionID != "conn-1" || r.Type != "permission:response" || !r.Decisive {
		t.Fatalf("round-trip lost fields: %+v", r)
	}
}

func TestWSReceiveActionValidate(t *testing.T) {
	if err := (WSReceiveAction{ConnectionID: "c", Type: "t"}).Validate(); err != nil {
		t.Fatalf("valid action rejected: %v", err)
	}
	if err := (WSReceiveAction{ConnectionID: "", Type: "t"}).Validate(); err == nil {
		t.Fatal("empty connection_id should fail validation")
	}
	if err := (WSReceiveAction{ConnectionID: "c", Type: ""}).Validate(); err == nil {
		t.Fatal("empty type should fail validation")
	}
}

func TestWSDisconnectActionRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSDisconnectAction{ConnectionID: "conn-2"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if envelope.Type != ActionWSDisconnect {
		t.Fatalf("type = %s, want %s", envelope.Type, ActionWSDisconnect)
	}
	var d WSDisconnectAction
	if err := json.Unmarshal(envelope.Raw, &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.ConnectionID != "conn-2" {
		t.Fatalf("round-trip failed: %+v", d)
	}
}

// Guard against accidental JSON-tag drift by decoding raw bytes.
func TestWSReceiveActionJSONTags(t *testing.T) {
	raw := []byte(`{"connection_id":"c","type":"t","timeout":3,"decisive":true}`)
	var r WSReceiveAction
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Timeout != 3 || !r.Decisive {
		t.Fatalf("json tags mismatch: %+v", r)
	}
}

func TestWSConnectActionCredentialRefRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSConnectAction{
		URL:           "ws://x",
		ConnectionID:  "c1",
		CredentialRef: "bridge-actor",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var c WSConnectAction
	if err := json.Unmarshal(envelope.Raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.CredentialRef != "bridge-actor" {
		t.Fatalf("credential_ref round-trip lost: %+v", c)
	}
}

func TestWSConnectActionRoleRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSConnectAction{
		URL: "ws://x", ConnectionID: "c1", Role: "web",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var c WSConnectAction
	if err := json.Unmarshal(envelope.Raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.Role != "web" {
		t.Fatalf("role round-trip lost: %+v", c)
	}
}

func TestWSReceiveActionAssertRoundTrip(t *testing.T) {
	envelope, err := MarshalAction(&WSReceiveAction{
		ConnectionID: "c1", Type: "approval",
		Assert: map[string]any{"payload.approved": true},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var r WSReceiveAction
	if err := json.Unmarshal(envelope.Raw, &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.Assert["payload.approved"] != true {
		t.Fatalf("assert round-trip lost: %+v", r.Assert)
	}
}

func TestWSReceiveActionValidateRejectsEmptyAssertKey(t *testing.T) {
	a := WSReceiveAction{ConnectionID: "c1", Type: "x", Assert: map[string]any{"": true}}
	if err := a.Validate(); err == nil {
		t.Fatal("empty assert path key should be rejected")
	}
}

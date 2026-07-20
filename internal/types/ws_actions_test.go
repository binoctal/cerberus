package types

import (
	"encoding/json"
	"testing"
)

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
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := got.(WSReceiveAction)
	if !ok {
		t.Fatalf("deref type %T, want WSReceiveAction value", got)
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
	got, err := UnmarshalAction(envelope)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d, ok := got.(WSDisconnectAction); !ok || d.ConnectionID != "conn-2" {
		t.Fatalf("round-trip failed: %+v", got)
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

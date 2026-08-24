package project

import (
	"path/filepath"
	"testing"
)

// The realtime-e2e dogfood config is the replicas cardinality reference
// config: 3 expanded bridges, a bridge3 protocol role carrying the
// cardinality claim, and the claim in the ledger.
func TestDogfoodRealtimeE2EReplicas(t *testing.T) {
	cfg, err := LoadFromFile(filepath.Join("..", "..", "dogfood", "realtime-e2e", ".cerberus", "project.yaml"))
	if err != nil {
		t.Fatalf("load dogfood config: %v", err)
	}
	names := map[string]bool{}
	for _, a := range cfg.Actors {
		names[a.Name] = true
	}
	for _, want := range []string{"bridge-pty-1", "bridge-pty-2", "bridge-pty-3"} {
		if !names[want] {
			t.Fatalf("expanded actor %s missing (have %v)", want, names)
		}
	}
	role := cfg.Services[0].Protocol.Roles["bridge3"]
	if role == nil || role.CredentialRef != "bridge-pty-3" {
		t.Fatalf("bridge3 role wrong: %+v", role)
	}
	found := false
	for _, c := range cfg.Claims.Claims {
		if c.ID == "multi-device-orchestration" && c.ImpliesCardinality == 3 && c.Critical {
			found = true
		}
	}
	if !found {
		t.Fatal("multi-device-orchestration claim (cardinality 3, critical) missing from ledger")
	}
}

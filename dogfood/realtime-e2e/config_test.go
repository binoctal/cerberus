package main

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// TestProjectConfig_Loads pins the realtime-e2e dogfood surface: the shared
// open-agents protocol resolves, the web actor stays emulated, and ALL THREE
// bridges (replicas: 3 expansion) are real-process actors whose roles point
// at them (so deterministic generators never self-play a role a real process
// occupies).
func TestProjectConfig_Loads(t *testing.T) {
	cfg, err := project.LoadFromFile(".cerberus/project.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(cfg.Services); got != 1 || cfg.Services[0].Name != "realtime" {
		t.Fatalf("services=%+v", cfg.Services)
	}
	svc := cfg.Services[0]
	if svc.Protocol == nil {
		t.Fatal("protocol_ref not resolved into svc.Protocol")
	}
	if svc.Protocol.Framing != "json" {
		t.Fatalf("framing=%q want json", svc.Protocol.Framing)
	}

	if len(cfg.Actors) != 7 {
		t.Fatalf("actors=%d want 7 (web + web-rival + admin + 3 replica bridges + real-ACP bridge)", len(cfg.Actors))
	}
	if cfg.Actors[0].Name != "web-actor" || (cfg.Actors[0].Fidelity != project.FidelityEmulated && cfg.Actors[0].Fidelity != "") {
		t.Fatalf("web actor=%+v", cfg.Actors[0])
	}
	// admin-actor (HTTP-only superadmin JWT) sorts between web and bridges.
	if cfg.Actors[1].Name != "admin-actor" {
		t.Fatalf("actor[1]=%+v want admin-actor", cfg.Actors[1])
	}
	// web-rival-actor (HTTP-only rival principal for the -crossuser tier).
	if cfg.Actors[2].Name != "web-rival-actor" {
		t.Fatalf("actor[2]=%+v want web-rival-actor", cfg.Actors[2])
	}
	for i, want := range []string{"bridge-pty-1", "bridge-pty-2", "bridge-pty-3"} {
		a := cfg.Actors[i+3]
		if a.Name != want || a.Fidelity != project.FidelityRealProcess || a.Process == nil {
			t.Fatalf("actor[%d]=%+v", i+3, a)
		}
		if len(a.Process.Setup) == 0 || len(a.Process.Start) == 0 {
			t.Fatalf("actor %s missing setup/start: %+v", a.Name, a.Process)
		}
		if a.Process.CaptureJSON["deviceId"] == "" || a.Process.CaptureFile == "" {
			t.Fatalf("actor %s missing deviceId capture: %+v", a.Name, a.Process)
		}
	}

	// Roles bind to the real actors so the generator suppression and
	// {{bridge.deviceId}} templating have their anchors.
	for role, wantActor := range map[string]string{"bridge": "bridge-pty-1", "bridge2": "bridge-pty-2", "bridge3": "bridge-pty-3", "bridgeacp": "bridge-acp-real"} {
		r := svc.Protocol.Roles[role]
		if r == nil || r.CredentialRef != wantActor {
			t.Fatalf("role %s=%+v want credential_ref %s", role, r, wantActor)
		}
	}
	// ACP layer declarations: replicas fake, bridgeacp real.
	if r := svc.Protocol.Roles["bridge"]; r.ACPCli != "claude" || r.ACPReal {
		t.Fatalf("bridge acp decl = %+v", svc.Protocol.Roles["bridge"])
	}
	if r := svc.Protocol.Roles["bridgeacp"]; r.ACPCli != "claude" || !r.ACPReal {
		t.Fatalf("bridge-acp acp decl = %+v", r)
	}
}

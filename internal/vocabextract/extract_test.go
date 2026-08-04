package vocabextract

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestExtract_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "stub.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		Edges []struct {
			Type string `json:"type"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	if len(got.Edges) == 0 {
		t.Fatalf("no edges in stub output: %s", out)
	}
	if got.Edges[0].Type != "stub:type" {
		t.Errorf("edge0 type = %q, want stub:type", got.Edges[0].Type)
	}
}

func TestExtract_FallThrough(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	check := func(t *testing.T, out []byte, want ...string) {
		t.Helper()
		var got struct{ Edges []map[string]any }
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, e := range got.Edges {
			if v, _ := e["type"].(string); v != "" {
				seen[v] = true
			}
		}
		for _, w := range want {
			if !seen[w] {
				t.Errorf("missing edge type %q in %d edges", w, len(got.Edges))
			}
		}
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "switch-fallthrough.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	check(t, out, "encrypted", "session:created", "session:started", "workflow:task_progress")

	out, err = Extract(context.Background(), filepath.Join("testdata", "sendtobridge.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct{ Edges []map[string]any }
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range got.Edges {
		if e["type"] == "session:start" &&
			e["from_role"] == "web" && e["to_role"] == "bridge" &&
			e["trigger"] == "message_handled" &&
			e["guard"] == "meta.type === 'web'" {
			found = true
		}
	}
	if !found {
		t.Errorf("no web->bridge session:start edge: %+v", got.Edges)
	}
}

func TestExtract_SideEffectsAndBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "sideeffect.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type        string `json:"type"`
			SideEffects []struct {
				Kind      string   `json:"kind"`
				WhenTypes []string `json:"when_types"`
			} `json:"side_effects"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var progress *struct {
		Type        string `json:"type"`
		SideEffects []struct {
			Kind      string   `json:"kind"`
			WhenTypes []string `json:"when_types"`
		} `json:"side_effects"`
	}
	for i := range got.Edges {
		if got.Edges[i].Type == "workflow:task_progress" {
			progress = &got.Edges[i]
		}
	}
	if progress == nil {
		t.Fatal("no workflow:task_progress edge")
	}
	if len(progress.SideEffects) != 1 || progress.SideEffects[0].Kind != "notify_orchestrator" {
		t.Errorf("side_effects = %+v", progress.SideEffects)
	}

	out, err = Extract(context.Background(), filepath.Join("testdata", "batch.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var b struct {
		Edges []struct {
			Type    string `json:"type"`
			Partial bool   `json:"partial"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatal(err)
	}
	var anyPartial bool
	for _, e := range b.Edges {
		if e.Partial {
			anyPartial = true
		}
	}
	if !anyPartial {
		t.Errorf("no partial edge emitted for batch fixture: %+v", b.Edges)
	}
}

func TestExtract_UnmatchedNotify(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "notify-unmatched.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type        string `json:"type"`
			Unsupported bool   `json:"unsupported"`
			SideEffects []struct {
				Kind string `json:"kind"`
			} `json:"side_effects"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var anyUnsupported bool
	for _, e := range got.Edges {
		if e.Unsupported {
			anyUnsupported = true
			continue
		}
		// Relay (broadcastToWeb) edges must NOT inherit a spurious
		// side_effect from the unmatched notifyOrchestrator call.
		if len(e.SideEffects) != 0 {
			t.Errorf("edge %q got spurious side_effects: %+v", e.Type, e.SideEffects)
		}
	}
	if !anyUnsupported {
		t.Errorf("expected an unsupported:true stub for unmatched notify, got: %+v", got.Edges)
	}
}

func TestExtract_PreconditionRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "session-send-gate.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			FromRole       string `json:"from_role"`
			ToRole         string `json:"to_role"`
			Type           string `json:"type"`
			RouteField     string `json:"route_field"`
			OnMissingRoute *struct {
				Kind string `json:"kind"`
				Code string `json:"code"`
			} `json:"on_missing_route"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	// The web->web broadcast edge must declare the deviceId precondition
	// honestly (route_field + on_missing_route), not hide behind a plain
	// broadcast_web with no routing metadata.
	var webToWeb *struct {
		FromRole       string `json:"from_role"`
		ToRole         string `json:"to_role"`
		Type           string `json:"type"`
		RouteField     string `json:"route_field"`
		OnMissingRoute *struct {
			Kind string `json:"kind"`
			Code string `json:"code"`
		} `json:"on_missing_route"`
	}
	for i := range got.Edges {
		if got.Edges[i].FromRole == "web" && got.Edges[i].ToRole == "web" && got.Edges[i].Type == "session:send" {
			webToWeb = &got.Edges[i]
		}
	}
	if webToWeb == nil {
		t.Fatalf("no web->web session:send edge: %+v", got.Edges)
	}
	if webToWeb.RouteField != "payload.deviceId" {
		t.Errorf("web->web route_field = %q, want payload.deviceId", webToWeb.RouteField)
	}
	if webToWeb.OnMissingRoute == nil || webToWeb.OnMissingRoute.Code != "MISSING_DEVICE_ID" {
		t.Errorf("web->web on_missing_route = %+v, want code MISSING_DEVICE_ID", webToWeb.OnMissingRoute)
	}
}

func TestExtract_ExcludeSender(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "exclude-sender.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type     string `json:"type"`
			Delivery struct {
				Mode          string `json:"mode"`
				ExcludeSender bool   `json:"exclude_sender"`
			} `json:"delivery"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	for _, e := range got.Edges {
		if e.Type == "echo-all" && !e.Delivery.ExcludeSender {
			t.Errorf("echo-all (broadcastToWeb(msg, ws)) must have exclude_sender=true: %+v", e.Delivery)
		}
		if e.Type == "echo-everyone" && e.Delivery.ExcludeSender {
			t.Errorf("echo-everyone (broadcastToWeb(msg)) must have exclude_sender=false: %+v", e.Delivery)
		}
	}
	var hasEchoAll, hasEchoEveryone bool
	for _, e := range got.Edges {
		if e.Type == "echo-all" {
			hasEchoAll = true
		}
		if e.Type == "echo-everyone" {
			hasEchoEveryone = true
		}
	}
	if !hasEchoAll || !hasEchoEveryone {
		t.Fatalf("missing edges; echo-all=%v echo-everyone=%v in %+v", hasEchoAll, hasEchoEveryone, got.Edges)
	}
}

func TestExtract_LifecycleTriggers(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	find := func(out []byte, wantType string) (map[string]any, bool) {
		var got struct {
			Edges []map[string]any `json:"edges"`
		}
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		for _, e := range got.Edges {
			if e["type"] == wantType {
				return e, true
			}
		}
		return nil, false
	}

	out, err := Extract(context.Background(), filepath.Join("testdata", "lifecycle-fetch.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	e, ok := find(out, "broadcast:lifecycle")
	if !ok {
		t.Fatalf("no broadcast:lifecycle edge: %s", out)
	}
	if e["trigger"] != "fetch_branch" {
		t.Errorf("fetch trigger = %v, want fetch_branch", e["trigger"])
	}
	if e["from_role"] != nil {
		t.Errorf("fetch from_role = %v, want null", e["from_role"])
	}

	out, err = Extract(context.Background(), filepath.Join("testdata", "lifecycle-disconnect.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	e, ok = find(out, "device:offline")
	if !ok {
		t.Fatalf("no device:offline edge: %s", out)
	}
	if e["trigger"] != "disconnect_bridge" {
		t.Errorf("webSocketClose trigger = %v, want disconnect_bridge", e["trigger"])
	}
	if e["from_role"] != "bridge" {
		t.Errorf("webSocketClose from_role = %v, want bridge", e["from_role"])
	}
}

func TestExtract_DynamicType(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "dynamic-type.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type       string `json:"type"`
			BestEffort bool   `json:"best_effort"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	var anyDynamic bool
	for _, e := range got.Edges {
		if e.Type == "(dynamic)" && e.BestEffort {
			anyDynamic = true
		}
	}
	if !anyDynamic {
		t.Errorf("expected a (dynamic) best_effort edge for non-literal broadcast arg, got: %+v", got.Edges)
	}
}

package vocabextract

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestExtract_SetMembership(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "set-membership.ts"))
	if err != nil {
		t.Skipf("node: %v", err)
	}
	var got struct {
		Edges []struct {
			Type       string `json:"type"`
			FromRole   string `json:"from_role"`
			ToRole     string `json:"to_role"`
			RouteField string `json:"route_field"`
			BestEffort bool   `json:"best_effort"`
			Partial    bool   `json:"partial"`
		} `json:"edges"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	byType := map[string][]struct {
		Type       string `json:"type"`
		FromRole   string `json:"from_role"`
		ToRole     string `json:"to_role"`
		RouteField string `json:"route_field"`
		BestEffort bool   `json:"best_effort"`
		Partial    bool   `json:"partial"`
	}{}
	for _, e := range got.Edges {
		byType[e.Type] = append(byType[e.Type], e)
	}
	// Set members resolve like case labels: one concrete edge each, no
	// best_effort flag (the batch sink keeps its unconditional best_effort).
	for _, w := range []string{"stub:b2w-one", "stub:b2w-two", "stub:w2b-one", "stub:special"} {
		if len(byType[w]) == 0 {
			t.Errorf("missing edge %q in %+v", w, got.Edges)
			continue
		}
		if byType[w][0].BestEffort {
			t.Errorf("edge %q should not be best_effort", w)
		}
	}
	if e := byType["stub:b2w-one"]; len(e) > 0 && (e[0].FromRole != "bridge" || e[0].ToRole != "web") {
		t.Errorf("stub:b2w-one roles = %s->%s", e[0].FromRole, e[0].ToRole)
	}
	if e := byType["stub:w2b-one"]; len(e) > 0 && (e[0].FromRole != "web" || e[0].ToRole != "bridge") {
		t.Errorf("stub:w2b-one roles = %s->%s", e[0].FromRole, e[0].ToRole)
	}
	// The Set-gated w2b edge keeps its route enrichment.
	if e := byType["stub:w2b-one"]; len(e) > 0 && e[0].RouteField != "payload.deviceId" {
		t.Errorf("stub:w2b-one route_field = %q", e[0].RouteField)
	}
	// The if (msg.type === ...) batch sink stays partial with a concrete type.
	if e := byType["stub:batched"]; len(e) > 0 && !e[0].Partial {
		t.Errorf("stub:batched should be partial")
	}
	// Nothing collapsed to (dynamic).
	if len(byType["(dynamic)"]) != 0 {
		t.Errorf("unexpected (dynamic) edges: %+v", byType["(dynamic)"])
	}
}

func TestExtract_HonoRoutes(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "hono", "worker.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		HTTPRoutes []struct {
			Method  string `json:"method"`
			Path    string `json:"path"`
			Mount   string `json:"mount"`
			Partial bool   `json:"partial"`
		} `json:"http_routes"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
		SkippedOn int `json:"skipped_on_registrations"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	seen := map[string]int{}
	for _, r := range got.HTTPRoutes {
		seen[r.Method+" "+r.Path]++
	}
	for _, w := range []string{
		"GET /health",
		"POST /api/dev/setup",
		// The worker fixture mounts its router through an ALIASED named
		// import (thingRoutes as stuffRoutes): these three assertions are the
		// regression guard that the local alias name resolves to the source
		// file and the mounted routes do not silently vanish.
		"GET /api/things",
		"GET /api/things/:id",
		"DELETE /api/things/nested/jobs/*",
	} {
		if seen[w] == 0 {
			t.Errorf("route %q missing; got %+v", w, got.HTTPRoutes)
		}
	}
	if seen["POST /api/dev/setup"] != 1 {
		t.Errorf("duplicate registration must merge to one entry, got %d", seen["POST /api/dev/setup"])
	}
	// Dev backdoor veto: provisioning endpoints (/api/dev/*, /api/auth/dev/*)
	// create or rotate identity state. The sweep firing one mid-run (run32:
	// POST /api/dev/setup recreated the dev user and invalidated the browser
	// session's token, bricking the whole UI leg) must not happen — the
	// extractor marks them partial so no cases generate and they leave the
	// coverage denominator.
	for _, r := range got.HTTPRoutes {
		if r.Method == "POST" && r.Path == "/api/dev/setup" && !r.Partial {
			t.Error("POST /api/dev/setup must be partial (dev backdoor veto)")
		}
		if r.Method == "GET" && r.Path == "/api/things" && r.Partial {
			t.Error("GET /api/things must not be partial")
		}
	}
	// Destructive identity veto: POST /api/auth/delete-account wipes the
	// acting user and cascades every resource they own. The run32 sweep fired
	// it with the real web JWT (200, case "passed") and deleted the dev user
	// mid-run — bridge devices 404'd, WS connections 401'd, the browser
	// session's token orphaned, and the whole UI leg failed on the login page.
	// It must never enter the sweep.
	for _, r := range got.HTTPRoutes {
		if r.Path == "/api/auth/delete-account" && !r.Partial {
			t.Error("POST /api/auth/delete-account must be partial (destructive identity veto)")
		}
	}
	if seen["PUT /secret"] != 0 {
		t.Error("unmounted route leaked into http_routes")
	}
	if seen["GET /multi"] != 0 {
		t.Error("app.on registration extracted despite v1 skip")
	}
	if got.SkippedOn != 1 {
		t.Errorf("skipped_on_registrations = %d, want 1", got.SkippedOn)
	}
	fileSet := map[string]bool{}
	for _, f := range got.Files {
		fileSet[f.Path] = true
	}
	for _, w := range []string{"worker.ts", "things.ts", "nested"} {
		found := false
		for p := range fileSet {
			if strings.Contains(filepath.ToSlash(p), w) {
				found = true
			}
		}
		if !found {
			t.Errorf("traversed file %q missing from files output: %+v", w, got.Files)
		}
	}
	for p := range fileSet {
		if strings.Contains(p, "unmounted") {
			t.Errorf("unmounted file %q must not be traversed", p)
		}
	}
}

// TestExtract_AnonUseMiddlewares: a glob use prefix ('/api/*', bare '*')
// applies to every path under the stripped prefix, and anonymous inline
// app.use middlewares are captured under a stable synthesized name
// ('use:/api/*', 'use:/' for the path-less form), deduped with '#N'
// suffixes when repeated within one file. This is the real open-agents
// shape: its JWT gate is an anonymous app.use('/api/*', async ...).
func TestExtract_AnonUseMiddlewares(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "hono", "use-anon.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		HTTPRoutes []struct {
			Method      string   `json:"method"`
			Path        string   `json:"path"`
			Middlewares []string `json:"middlewares"`
		} `json:"http_routes"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	has := func(mws []string, name string) bool {
		for _, m := range mws {
			if m == name {
				return true
			}
		}
		return false
	}
	for _, r := range got.HTTPRoutes {
		// The named '*' use applies to every route, glob or not.
		if !has(r.Middlewares, "requestLogger") {
			t.Errorf("%s %s must carry requestLogger (bare '*' glob), got %v", r.Method, r.Path, r.Middlewares)
		}
		if !strings.HasPrefix(r.Path, "/api") {
			continue
		}
		// Both anonymous /api/* uses apply under the stripped prefix; the
		// second gets the '#2' suffix, not a name collision.
		if !has(r.Middlewares, "use:/api/*") {
			t.Errorf("%s %s must carry use:/api/* (anonymous use, glob prefix), got %v", r.Method, r.Path, r.Middlewares)
		}
		if !has(r.Middlewares, "use:/api/*#2") {
			t.Errorf("%s %s must carry use:/api/*#2 (deduped second anonymous use), got %v", r.Method, r.Path, r.Middlewares)
		}
	}
	// Outside the glob: no /api/* middleware leaks onto the route.
	for _, r := range got.HTTPRoutes {
		if r.Path == "/health" && (has(r.Middlewares, "use:/api/*") || has(r.Middlewares, "use:/api/*#2")) {
			t.Errorf("/health must not carry /api/* middlewares, got %v", r.Middlewares)
		}
	}
	if len(got.HTTPRoutes) != 3 {
		t.Fatalf("http_routes = %+v, want 3 routes", got.HTTPRoutes)
	}
}

// TestExtract_ZodMinBodies pins the rich-shape zod extraction and the
// handler-side parse pattern (run35's dominant failure family: ~150 admin
// mutations sent EMPTY bodies because their schemas — .min(), enums, arrays,
// nested objects, or plain `schema.parse(c.req.json())` without a zValidator
// middleware — extracted nothing).
func TestExtract_ZodMinBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns node")
	}
	out, err := Extract(context.Background(), filepath.Join("testdata", "hono", "worker.ts"))
	if err != nil {
		t.Skipf("node unavailable or npm failed: %v", err)
	}
	var got struct {
		HTTPRoutes []struct {
			Method  string         `json:"method"`
			Path    string         `json:"path"`
			MinBody map[string]any `json:"min_body"`
		} `json:"http_routes"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("parse extractor stdout: %v\nraw=%s", err, out)
	}
	byRoute := map[string]map[string]any{}
	for _, r := range got.HTTPRoutes {
		byRoute[r.Method+" "+r.Path] = r.MinBody
	}

	// Rich shapes: every required field with a refinement-satisfying value;
	// optional and defaulted fields omitted.
	rich := byRoute["POST /zod/api/rich"]
	if rich == nil {
		t.Fatalf("POST /zod/api/rich must carry min_body, got none (routes: %v)", byRoute)
	}
	wantRich := map[string]any{
		"name": "x", "email": "x@x.com", "role": "admin",
		"age": float64(0), "tags": []any{"x"}, "addr": map[string]any{"city": "x"},
	}
	if len(rich) != len(wantRich) {
		t.Fatalf("rich min_body = %v (bio/status must be omitted)", rich)
	}
	for k, v := range wantRich {
		gotV, ok := rich[k]
		if !ok {
			t.Fatalf("rich min_body missing %q: %v", k, rich)
		}
		switch want := v.(type) {
		case []any:
			arr, ok := gotV.([]any)
			if !ok || len(arr) != 1 || arr[0] != want[0] {
				t.Fatalf("rich[%s] = %v, want %v", k, gotV, want)
			}
		case map[string]any:
			m, ok := gotV.(map[string]any)
			if !ok || m["city"] != want["city"] {
				t.Fatalf("rich[%s] = %v, want %v", k, gotV, want)
			}
		default:
			if gotV != v {
				t.Fatalf("rich[%s] = %v, want %v", k, gotV, v)
			}
		}
	}

	// Handler-side schema.parse (no zValidator middleware).
	parse := byRoute["POST /zod/api/parse"]
	if parse == nil {
		t.Fatalf("POST /zod/api/parse must carry min_body from the .parse() schema, got none")
	}
	if parse["title"] != "x" || parse["n"] != float64(0) {
		t.Fatalf("parse min_body = %v, want {title:x, n:0}", parse)
	}

	// Unextractable in either position: omit, never guess.
	if m := byRoute["POST /zod/api/picky-parse"]; m != nil {
		t.Fatalf("unextractable .parse() schema must omit min_body, got %v", m)
	}
	if m := byRoute["POST /zod/api/picky"]; m != nil {
		t.Fatalf("unextractable zValidator schema must omit min_body, got %v", m)
	}
}

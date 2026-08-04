# Protocol Vocabulary v2 — Fidelity Gaps + Scout Wiring Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the four fidelity gaps left in v1 of the protocol-vocabulary extractor (session:send deviceId gating, exclude_sender, lifecycle trigger coverage, synthetic-type coverage) and wire the vocabulary into Scout's planning prompt so the planning phase can reason about WS message flow.

**Architecture:** Tasks 1–4 are pure-AST changes to `internal/vocabextract/extractor.mjs` plus fixture-driven Go tests — no LLM, deterministic. Task 5 loads `project.Vocabulary` alongside `Protocol` (same ref name) and renders a compact, direction-grouped routing summary into Scout's planning context via a new `renderVocabSummary` helper. The dynamic `TestVocabularyDriven` is migrated off its `from_role == web` workaround to be vocab (`RouteField`) driven.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`; node + `ts-morph` 25.0.0 (dev-time subprocess only).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`; **pure Go, no cgo** (`extractor.mjs` runs as a node subprocess — does not violate the cgo ban).
- Commit author `binoctal <binoctal@gmail.com>`, **no** `Co-Authored-By`. Use `git -c user.name=binoctal -c user.email=binoctal@gmail.com commit`.
- All documentation under `cerberus-docs/`; commit messages & code comments in English; follow existing comment density/naming.
- `VocabEdge` and friends carry **both** `yaml` and `json` struct tags (extractor stdout is JSON; on-disk file is YAML) — do not drop either tag.
- The extractor is pure AST: never invent `transforms_to`, never call an LLM. Partial/batch edges are `partial: true` + skip; unrecognized shapes are `unsupported: true` + skip.
- Node v20.20.0 + npm must be on PATH for extractor tests (ts-morph 25.0.0 cached). If `node` is absent, extractor tests `t.Skip`.
- The integration test (`TestVocabularyDriven`) additionally needs the open-agents dev server on `:8989`; if unreachable, it `t.Skip`s. The server is only needed to *verify* Task 1's integration-test edit — the edit itself does not require it.
- **Workspace: feature branch `feat/protocol-vocabulary-v2` on the main checkout, NOT a git worktree.** Dogfood/agent tests resolve `../open-agents` as a sibling directory; a worktree breaks that path resolution.

## File Structure

- **Modify** `internal/vocabextract/extractor.mjs` — Tasks 1, 2, 3, 4 (each an isolated, additive detector/helper).
- **Modify** `internal/vocabextract/extract_test.go` — Tasks 1, 2, 3, 4 add fixture-driven subtests.
- **Create** `internal/vocabextract/testdata/session-send-gate.ts` — Task 1 fixture.
- **Create** `internal/vocabextract/testdata/exclude-sender.ts` — Task 2 fixture.
- **Create** `internal/vocabextract/testdata/lifecycle-fetch.ts`, `internal/vocabextract/testdata/lifecycle-disconnect.ts` — Task 3 fixtures.
- **Create** `internal/vocabextract/testdata/dynamic-type.ts` — Task 4 fixture.
- **Modify** `internal/project/loader.go` — Task 5 auto-loads `<name>.vocab.yaml` alongside the protocol ref.
- **Modify** `internal/project/schema.go` — Task 5 adds `Service.Vocabulary`.
- **Create** `internal/head/scout/vocab_context.go` — Task 5 `renderVocabSummary`.
- **Create** `internal/head/scout/vocab_context_test.go` — Task 5 unit test.
- **Modify** `internal/head/scout/direct_planning.go` — Task 5 wires the summary into `buildPlanningContext`.
- **Modify** `internal/head/agent/vocabulary_driven_test.go` — Task 1 migrates off the `from_role == web` workaround.
- **Modify** `dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml` — Task 1 regenerates with the new detectors (session:send web→web gains `route_field` + `on_missing_route`; broadcast edges gain `exclude_sender`).

---

### Task 1: extractor — session:send web→web deviceId gating (precondition guard)

**Why:** In `room.ts`, `session:send`'s `broadcastToWeb(msg, ws)` (the web→web edge) only fires after an `if (!payload.deviceId) { sendError(ws,'MISSING_DEVICE_ID',...); break }` precondition guard. v1 emits this edge as plain `broadcast_web` with no `route_field`/`on_missing_route`, so the vocab hides the deviceId contract. The dynamic test works around it with a `from_role == web` heuristic (`vocabulary_driven_test.go:66-69`). This task makes the extractor describe the guard honestly, then migrates the test onto `RouteField`.

**Order matters (known pitfall):** the extractor change + fixture test must land and the dogfood vocab must be regenerated BEFORE the integration test is edited — otherwise the integration test goes red because the committed vocab does not yet carry `route_field` on the web→web edge.

**Files:**
- Create: `internal/vocabextract/testdata/session-send-gate.ts`
- Modify: `internal/vocabextract/extractor.mjs`
- Modify: `internal/vocabextract/extract_test.go`
- Modify: `internal/head/agent/vocabulary_driven_test.go`
- Modify: `dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml` (regenerated)

**Interfaces:**
- Produces (extractor JSON): edges may now carry `route_field` + `on_missing_route` when reached through a `!payload.<field>` precondition guard — including `broadcast_web` edges. No schema change (`VocabEdge.RouteField` / `OnMissingRoute` already exist in `internal/project/vocabulary.go`).

- [ ] **Step 1: Write the fixture**

Create `internal/vocabextract/testdata/session-send-gate.ts` (mirrors `room.ts` session:send shape — precondition guard as a preceding sibling, then route + broadcast):

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'session:send':
        if (meta.type === 'web') {
          const payload = msg.payload;
          if (!payload.deviceId) {
            this.sendError(ws, 'MISSING_DEVICE_ID', 'Cannot route message without deviceId');
            break;
          }
          this.sendToBridge(payload.deviceId, msg);
          this.broadcastToWeb(msg, ws);
        }
        break;
      default:
    }
  }
  sendToBridge(deviceId, msg) {}
  broadcastToWeb(msg, ws) {}
  sendError(ws, code, reason) {}
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/vocabextract/extract_test.go`:

```go
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
			FromRole      string `json:"from_role"`
			ToRole        string `json:"to_role"`
			Type          string `json:"type"`
			RouteField    string `json:"route_field"`
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
		FromRole      string `json:"from_role"`
		ToRole        string `json:"to_role"`
		Type          string `json:"type"`
		RouteField    string `json:"route_field"`
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
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/vocabextract/ -run TestExtract_PreconditionRoute -v`
Expected: FAIL — the web→web edge has empty `route_field` (v1 never described the precondition).

- [ ] **Step 4: Implement the precondition detector in extractor.mjs**

In `internal/vocabextract/extractor.mjs`, add a helper next to `missingRouteOf`:

```js
// preconditionRouteOf: detect a preceding-sibling guard of the form
// `if (!payload.<field>) { ...sendError(ws,'<CODE>',...)...; break }` in the
// same block as the emit call. This is session:send's deviceId gate: the
// route/broadcast only fires when payload.<field> is present, else sendError.
// Unlike missingRouteOf (if/else around the call), the guard here is a prior
// statement in the enclosing block. Returns {route_field, on_missing_route}.
function preconditionRouteOf(call) {
  const block = call.getFirstAncestorByKind(SyntaxKind.Block);
  if (!block) return null;
  const stmts = block.getStatements();
  const ownStmt = call.getFirstAncestorByKind(SyntaxKind.ExpressionStatement);
  let idx = -1;
  for (let i = 0; i < stmts.length; i++) {
    if (stmts[i] === ownStmt) { idx = i; break; }
  }
  for (let i = idx - 1; i >= 0; i--) {
    const s = stmts[i];
    if (s.getKind() !== SyntaxKind.IfStatement) continue;
    const m = s.getExpression().getText().match(/!\s*payload\.(\w+)/);
    if (!m) continue;
    const thenStmt = s.getThenStatement();
    if (!thenStmt) continue;
    const errs = thenStmt.getDescendantsOfKind(SyntaxKind.CallExpression)
      .filter(c => c.getExpression().getText().endsWith('sendError'));
    if (errs.length === 0) continue;
    const code = errs[0].getArguments()[1]?.getText().replace(/^['"`]|['"`]$/g, '') || '';
    return { route_field: 'payload.' + m[1], on_missing_route: { kind: 'send_error', code } };
  }
  return null;
}
```

Then unify route enrichment. Add a helper and call it for every pushed edge (both the `if (cc)` branch and the `else` branch), replacing the inline `isW2B`-only route logic:

```js
// enrichRoute attaches route_field / on_missing_route to an edge from the
// call site. sendToBridge carries route_field in its first arg; either shape
// may additionally sit behind a !payload.<field> precondition guard.
function enrichRoute(e, call, isW2B) {
  if (isW2B) {
    const rf = routeFieldOf(call);
    if (rf) e.route_field = rf;
    const mr = missingRouteOf(call);
    if (mr) e.on_missing_route = mr;
  }
  const pre = preconditionRouteOf(call);
  if (pre) {
    if (!e.route_field) e.route_field = pre.route_field;
    if (!e.on_missing_route) e.on_missing_route = pre.on_missing_route;
  }
}
```

In the `if (cc)` branch, replace the existing `if (isW2B) { ... }` block with `enrichRoute(e, call, isW2B);` placed after `const e = make(t);` and before `edges.push(e);`. In the `else` branch, replace its `if (isW2B) { ... }` block with `enrichRoute(edge, call, isW2B);` before `edges.push(edge);`.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/vocabextract/ -run TestExtract_PreconditionRoute -v`
Expected: PASS.

Also run the full vocabextract suite to confirm no regression:
Run: `go test ./internal/vocabextract/ -v`
Expected: all PASS.

- [ ] **Step 6: Commit the extractor change**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/testdata/session-send-gate.ts internal/vocabextract/extract_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): describe session:send deviceId precondition as route_field"
```

- [ ] **Step 7: Regenerate the dogfood vocab**

Build and regenerate so the committed vocab carries the new `route_field`/`on_missing_route` on session:send's web→web edge:

```bash
make build
./build/cerberus protocol vocabulary \
  --name open-agents \
  --from ../open-agents/apps/api/src/realtime/room.ts
```
Answer `y` at the overwrite prompt (output path is resolved relative to the dogfood project dir — run from `dogfood/ws-realtime/` if the relative `--from` does not resolve from the repo root, i.e. `cd dogfood/ws-realtime && ../../build/cerberus protocol vocabulary --name open-agents --from ../../../open-agents/apps/api/src/realtime/room.ts`).

Verify the web→web session:send edge now carries `route_field: payload.deviceId` and `on_missing_route`:
```bash
awk '/type: session:send/{n=NR} n&&NR<=n+12' dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml | grep -A3 "to_role: web"
```
Expected: the `to_role: web` / `type: session:send` block contains `route_field: payload.deviceId` and an `on_missing_route:` block.

- [ ] **Step 8: Commit the regenerated vocab**

```bash
git add dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "dogfood(ws-realtime): regenerate vocab with session:send route_field"
```

- [ ] **Step 9: Migrate the integration test off the workaround**

In `internal/head/agent/vocabulary_driven_test.go`, replace the `from_role == web` heuristic with vocab (`RouteField`) driven payload construction. Add `"encoding/json"` and `"strings"` to the import block. Replace the message-construction block (currently the `msg := ...; if e.FromRole == "web" { ... }` lines) with:

```go
				// Build the outbound message. Edges that declare a route_field
				// (e.g. payload.deviceId) require that field present or the DO
				// rejects with MISSING_DEVICE_ID before relaying; the vocab now
				// describes this, so payload shape is driven by RouteField
				// rather than a from_role heuristic.
				msg := fmt.Sprintf(`{"type":%q}`, e.Type)
				if e.RouteField != "" {
					field := strings.TrimPrefix(e.RouteField, "payload.")
					body, err := json.Marshal(map[string]any{
						"type":    e.Type,
						"payload": map[string]any{field: f.deviceId},
					})
					if err != nil {
						t.Fatalf("marshal msg: %v", err)
					}
					msg = string(body)
				}
```

Remove the old multi-line comment that documented the workaround (the block starting `// The DO's room.ts gates every web-sourced message...`).

- [ ] **Step 10: Commit the test migration**

```bash
git add internal/head/agent/vocabulary_driven_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "test(agent): drive vocab relay payload from RouteField, not from_role"
```

**Verification checkpoint (run with the open-agents dev server up on :8989):**
```bash
go test -tags integration -run TestVocabularyDriven ./internal/head/agent/ -v
```
Expected: green; the `session:send` web→web sub-case passes driven by `RouteField`. (If the server is down this skip-checks; the orchestrator runs the live check between tasks.)

---

### Task 2: extractor — emit delivery.exclude_sender

**Why:** `broadcastToWeb(msg, ws)` — the optional second argument `ws` — excludes the sender from the fan-out (`room.ts:504` `private broadcastToWeb(msg, excludeWs?)`). v1 emits `exclude_sender: null` unconditionally and never sets it. session:send's web→web edge is the real consumer.

**Files:**
- Create: `internal/vocabextract/testdata/exclude-sender.ts`
- Modify: `internal/vocabextract/extractor.mjs`
- Modify: `internal/vocabextract/extract_test.go`

**Interfaces:**
- Produces: `delivery.exclude_sender: true` on broadcast edges whose `broadcastToWeb` call has a second argument. Maps to existing `VocabDelivery.ExcludeSender`.

- [ ] **Step 1: Write the fixture**

Create `internal/vocabextract/testdata/exclude-sender.ts`:

```ts
class UserRoom {
  handleMessage(ws, meta, msg) {
    switch (msg.type) {
      case 'echo-all':
        if (meta.type === 'web') { this.broadcastToWeb(msg, ws); }
        break;
      case 'echo-everyone':
        if (meta.type === 'bridge') { this.broadcastToWeb(msg); }
        break;
      default:
    }
  }
  broadcastToWeb(msg, excludeWs) {}
}
```

- [ ] **Step 2: Write the failing test**

Append to `internal/vocabextract/extract_test.go`:

```go
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
	byType := map[string]bool{}
	for _, e := range got.Edges {
		byType[e.Type+":"] = false
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
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/vocabextract/ -run TestExtract_ExcludeSender -v`
Expected: FAIL — `echo-all` has `exclude_sender: false` (v1 emits `null` which decodes to false).

- [ ] **Step 4: Implement exclude_sender detection in extractor.mjs**

Add a helper near `routeFieldOf`:

```js
// excludeSenderOf: broadcastToWeb(msg, ws?) excludes the originator when a
// second argument is present (the DO's private broadcastToWeb(msg, excludeWs)).
function excludeSenderOf(call, isB2W) {
  if (!isB2W) return false;
  return call.getArguments().length > 1;
}
```

In `make(type)`, replace the `delivery:` block so `exclude_sender` is derived from the call rather than hard-coded `null`. Because `make` is called before the call's arg shape is fully inspected in some branches, pass the call in. Change the `make` definition to `const make = (type) => ({ ... delivery: { mode: isB2W ? 'broadcast_web' : 'send_bridge_by_device', exclude_sender: excludeSenderOf(call, isB2W) }, ... })`. (The `call` variable is in scope where `make` is defined.)

In the batch detector's edge literal (the `edges.push({ ... delivery: { mode: 'broadcast_web', exclude_sender: null } ...})` block), leave `exclude_sender: null` — batch sinks (`batchOutput(...)`) take one arg, so exclude_sender is not applicable; keep the existing conservative null. (No behavior change there.)

Also extend the **dedup merge** so `exclude_sender` survives a merge (the merge block near the end of the extractor currently carries forward route_field/on_missing_route/partial/batch/best_effort/unsupported/side_effects but not delivery). Inside the `if (ex)` branch, after the existing field carry-forwards, add:

```js
    if (e.delivery && e.delivery.exclude_sender && !ex.delivery.exclude_sender) {
      ex.delivery.exclude_sender = true;
    }
```

This is defensive — session:send's web→web edge is unique today so no real collision occurs, but a future SUT with two same-key broadcast edges (one `broadcastToWeb(msg, ws)`, one `broadcastToWeb(msg)`) would otherwise silently lose the flag.

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/vocabextract/ -v`
Expected: all PASS (echo-all true, echo-everyone false).

- [ ] **Step 6: Regenerate the dogfood vocab so broadcast edges carry exclude_sender**

The acceptance criterion requires the committed `open-agents.vocab.yaml` to carry `exclude_sender: true` on broadcast edges that pass `ws` (notably session:send web→web). Regenerate (same command shape as Task 1 Step 7):

```bash
make build
cd dogfood/ws-realtime && ../../build/cerberus protocol vocabulary \
  --name open-agents \
  --from ../../../open-agents/apps/api/src/realtime/room.ts
```
Answer `y` at the overwrite prompt. Verify the session:send web→web edge now has `exclude_sender: true`:
```bash
grep -A4 "to_role: web" dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml | grep exclude_sender
```

- [ ] **Step 7: Commit**

```bash
git add internal/vocabextract/extractor.mjs internal/vocabextract/testdata/exclude-sender.ts internal/vocabextract/extract_test.go dogfood/ws-realtime/.cerberus/vocab/open-agents.vocab.yaml
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(vocabextract): emit delivery.exclude_sender for broadcastToWeb(msg, ws)"
```

---

### Task 3: extractor — cover fetch_branch / disconnect_bridge trigger mappings

**Why:** v1 wired `trigger = mname === 'fetch' ? 'fetch_branch' : mname === 'webSocketClose' ? 'disconnect_bridge' : ...` but no fixture exercises a `fetch` or `webSocketClose` emit point. The mapping is untested (dead-code risk). These are characterization tests — they pin existing behavior. If a fixture unexpectedly goes red, the trigger mapping is wrong and the extractor is fixed.

**Files:**
- Create: `internal/vocabextract/testdata/lifecycle-fetch.ts`
- Create: `internal/vocabextract/testdata/lifecycle-disconnect.ts`
- Modify: `internal/vocabextract/extract_test.go`

**Interfaces:** none new — exercises the existing `trigger`/`from_role` derivation.

- [ ] **Step 1: Write the fixtures**

Create `internal/vocabextract/testdata/lifecycle-fetch.ts` (mirrors the `/broadcast` endpoint path: a `fetch` method broadcasting an inline-typed message, no role guard → `from_role: null`, `trigger: fetch_branch`):

```ts
class UserRoom {
  async fetch(request) {
    this.broadcastToWeb({ type: 'broadcast:lifecycle' });
    return new Response('ok');
  }
  broadcastToWeb(msg) {}
}
```

Create `internal/vocabextract/testdata/lifecycle-disconnect.ts` (mirrors `webSocketClose` broadcasting `device:offline` under a bridge guard → `from_role: bridge`, `trigger: disconnect_bridge`):

```ts
class UserRoom {
  webSocketClose(ws) {
    const meta = ws.deserializeAttachment();
    if (meta.type === 'bridge') {
      this.broadcastToWeb({ type: 'device:offline' });
    }
  }
  broadcastToWeb(msg) {}
}
```

- [ ] **Step 2: Write the characterization test**

Append to `internal/vocabextract/extract_test.go`:

```go
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
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/vocabextract/ -run TestExtract_LifecycleTriggers -v`
Expected: PASS (characterizes the existing mapping). If RED, the trigger derivation is buggy — fix the extractor's `trigger` line and re-run.

- [ ] **Step 4: Commit**

```bash
git add internal/vocabextract/testdata/lifecycle-fetch.ts internal/vocabextract/testdata/lifecycle-disconnect.ts internal/vocabextract/extract_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "test(vocabextract): cover fetch_branch and disconnect_bridge trigger mappings"
```

---

### Task 4: extractor — cover the synthetic '(dynamic)' best-effort type

**Why:** When an emit point is not inside a `CaseClause` and its argument is not an object literal with a `type` property, the extractor falls back to `type: '(dynamic)'` with `best_effort: true`. That fallback branch has no assertion coverage. This characterizes it so regressions are caught.

**Files:**
- Create: `internal/vocabextract/testdata/dynamic-type.ts`
- Modify: `internal/vocabextract/extract_test.go`

**Interfaces:** none new — exercises the existing `else` (non-case) emit branch fallback.

- [ ] **Step 1: Write the fixture**

Create `internal/vocabextract/testdata/dynamic-type.ts` (a `fetch` handler that broadcasts a variable — no case, no literal `type` → synthetic type):

```ts
class UserRoom {
  async fetch(request) {
    const msg = await request.json();
    this.broadcastToWeb(msg);
    return new Response('ok');
  }
  broadcastToWeb(msg) {}
}
```

- [ ] **Step 2: Write the characterization test**

Append to `internal/vocabextract/extract_test.go`:

```go
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
			Type      string `json:"type"`
			BestEffort bool  `json:"best_effort"`
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
```

- [ ] **Step 3: Run the test**

Run: `go test ./internal/vocabextract/ -run TestExtract_DynamicType -v`
Expected: PASS (characterizes the fallback). If RED, the fallback path regressed — fix and re-run.

- [ ] **Step 4: Commit**

```bash
git add internal/vocabextract/testdata/dynamic-type.ts internal/vocabextract/extract_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "test(vocabextract): cover synthetic (dynamic) best-effort type fallback"
```

---

### Task 5: Scout — load Vocabulary and render a routing summary into the planning prompt

**Brainstorm conclusion (chosen design):** Vocab is loaded **alongside** the protocol by reusing the service's `protocol_ref` name — no new `vocab_ref` config field (the dogfood service already has `protocol_ref: open-agents`, and the vocab file is `open-agents.vocab.yaml`). Loading is best-effort: a missing vocab file is silently skipped (vocab is optional). Injection is **prompt-only**: a compact, direction-grouped summary rendered by one helper (`renderVocabSummary`) into **both** planners — the direct planner via `buildPlanningContext` and the ToT planner via a new `SetVocabSummary` setter (mirroring the existing `SetMemory` pattern) consumed in `buildProposeTask` — so the LLM can author `ws_*` choreography from concrete type names regardless of planning mode. Deterministic `ws_cases.go` generation is untouched (a deliberate v2 boundary; the dynamic `TestVocabularyDriven` already drives execution from vocab). Rendering shape (Option A, ~1–2 KB): one line per `(from→to, delivery)` group with inline type list; partial/unsupported/non-`message_handled` edges counted in a `[skipped: N ...]` footer so nothing is silently dropped.

**Files:**
- Modify: `internal/project/schema.go`
- Modify: `internal/project/loader.go`
- Create: `internal/head/scout/vocab_context.go`
- Create: `internal/head/scout/vocab_context_test.go`
- Modify: `internal/head/scout/direct_planning.go`

**Interfaces:**
- Produces: `project.Service.Vocabulary *Vocabulary` (loaded from `.cerberus/vocab/<protocol_ref>.vocab.yaml` when present).
- Produces: `scout.renderVocabSummary(services []project.Service) string` — grouped routing summary, empty string when no service has a Vocabulary.

- [ ] **Step 1: Add the Vocabulary field to Service**

In `internal/project/schema.go`, add to the `Service` struct (next to `Protocol`):

```go
	// Vocabulary optionally declares this service's WS routing vocabulary
	// (directed-edge model), loaded alongside Protocol from
	// .cerberus/vocab/<protocol_ref>.vocab.yaml. Nil when no vocab file exists.
	Vocabulary *Vocabulary `yaml:"-"`
```

The `yaml:"-"` tag is intentional: the vocabulary is always derived from a vocab file, never inline in project.yaml (mirrors how `Protocol` can be inline but `Vocabulary` is file-only).

- [ ] **Step 2: Write the loader + render tests first (TDD)**

Create `internal/head/scout/vocab_context_test.go`:

```go
package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRenderVocabSummary(t *testing.T) {
	services := []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{
			Source: project.VocabSource{ProtocolRef: "open-agents"},
			Edges: []project.VocabEdge{
				{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "send_bridge_by_device"}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "web", Type: "session:send", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web", ExcludeSender: true}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "bridge", Type: "session:output", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}, Partial: true},
				{FromRole: "bridge", ToRole: "web", Type: "device:offline", Trigger: "disconnect_bridge",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
			},
		},
	}}

	got := renderVocabSummary(services)
	for _, want := range []string{
		"WS Routing Vocabulary (realtime, 6 edges)",
		"bridge->web broadcast_web (2):",
		"session:created",
		"workflow:task_progress",
		"web->bridge send_bridge_by_device[route=payload.deviceId] (1):",
		"session:start",
		"web->web broadcast_web(exclude_sender)[route=payload.deviceId] (1):",
		"session:send",
		"[skipped: 2 partial/unsupported/non-message_handled edges]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- summary ---\n%s", want, got)
		}
	}
}

func TestRenderVocabSummary_Empty(t *testing.T) {
	if got := renderVocabSummary(nil); got != "" {
		t.Errorf("nil services = %q, want empty", got)
	}
	if got := renderVocabSummary([]project.Service{{Name: "svc"}}); got != "" {
		t.Errorf("service without vocabulary = %q, want empty", got)
	}
}

func TestBuildPlanningContextIncludesVocab(t *testing.T) {
	s := &Scout{config: &project.Config{Services: []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{
			Source: project.VocabSource{ProtocolRef: "open-agents"},
			Edges: []project.VocabEdge{{
				FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress", Trigger: "message_handled",
				Delivery: project.VocabDelivery{Mode: "broadcast_web"},
			}},
		},
	}}}}
	// buildPlanningContext -> buildPlanContext derefs model.API, so pass a
	// non-nil (empty) model rather than nil to avoid a panic.
	ctx := s.buildPlanningContext(&project.ProjectModel{}, "")
	if !strings.Contains(ctx, "WS Routing Vocabulary") ||
		!strings.Contains(ctx, "workflow:task_progress") {
		t.Errorf("planning context missing vocab summary\n--- context ---\n%s", ctx)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/head/scout/ -run 'TestRenderVocabSummary|TestBuildPlanningContextIncludesVocab' -v`
Expected: FAIL — `renderVocabSummary` undefined; `buildPlanningContext` does not yet render vocab.

- [ ] **Step 4: Implement renderVocabSummary**

Create `internal/head/scout/vocab_context.go`:

```go
package scout

import (
	"fmt"
	"sort"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// renderVocabSummary produces a compact, direction-grouped routing summary of
// every service's WS vocabulary for the planning prompt. It is prompt-only
// context: the LLM uses concrete type names to author ws_send/ws_receive
// choreography. Partial / unsupported / non-message_handled edges are counted
// in a footer rather than listed, so nothing is silently dropped. Returns ""
// when no service declares a vocabulary (byte-identical prompt for non-WS
// projects).
func renderVocabSummary(services []project.Service) string {
	var b strings.Builder
	for _, svc := range services {
		if svc.Vocabulary == nil || len(svc.Vocabulary.Edges) == 0 {
			continue
		}
		total := len(svc.Vocabulary.Edges)
		fmt.Fprintf(&b, "\n\n## WS Routing Vocabulary (%s, %d edges)\n", svc.Name, total)

		type group struct {
			label string
			from  string
			to    string
			mode  string
		}
		order := []group{}
		types := map[string][]string{}
		seen := map[string]map[string]bool{}
		skipped := 0

		for _, e := range svc.Vocabulary.Edges {
			if e.Partial || e.Unsupported || e.Trigger != "message_handled" {
				skipped++
				continue
			}
			label := e.Delivery.Mode
			if e.Delivery.ExcludeSender {
				label += "(exclude_sender)"
			}
			if e.RouteField != "" {
				label += fmt.Sprintf("[route=%s]", e.RouteField)
			}
			key := fmt.Sprintf("%s->%s %s", e.FromRole, e.ToRole, label)
			if _, ok := types[key]; !ok {
				order = append(order, group{label: key, from: e.FromRole, to: e.ToRole, mode: e.Delivery.Mode})
				types[key] = []string{}
				seen[key] = map[string]bool{}
			}
			if !seen[key][e.Type] {
				seen[key][e.Type] = true
				types[key] = append(types[key], e.Type)
			}
		}
		sort.Slice(order, func(i, j int) bool {
			if order[i].from != order[j].from {
				return order[i].from < order[j].from
			}
			if order[i].to != order[j].to {
				return order[i].to < order[j].to
			}
			return order[i].label < order[j].label
		})
		for _, g := range order {
			ts := types[g.label]
			fmt.Fprintf(&b, "%s (%d): %s\n", g.label, len(ts), strings.Join(ts, ", "))
		}
		if skipped > 0 {
			fmt.Fprintf(&b, "[skipped: %d partial/unsupported/non-message_handled edges]\n", skipped)
		}
	}
	return b.String()
}
```

- [ ] **Step 5: Wire the summary into buildPlanningContext**

In `internal/head/scout/direct_planning.go`, at the end of `buildPlanningContext` (just before `return planCtx`), append the vocab summary so it lands after the Services section and any memory:

```go
	planCtx += renderVocabSummary(s.config.Services)
```

`renderVocabSummary` returns "" when no service has a vocabulary, so non-WS projects get a byte-identical prompt.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/head/scout/ -run 'TestRenderVocabSummary|TestBuildPlanningContextIncludesVocab' -v`
Expected: PASS.

- [ ] **Step 7: Wire vocab into the ToT planner too (so both planners see it)**

The direct planner sees vocab via `buildPlanningContext` (Step 5). The ToT planner does not call `buildPlanningContext`; it builds its prompt in `buildProposeTask` from `formatModelForToT(model)`. Mirror the existing `memory` injection pattern (`ToTPlanner.memory` field + `SetMemory` setter + prepend in `buildProposeTask`):

In `internal/head/scout/tot.go`:
- Add a field to `ToTPlanner`: `vocabSummary string` (next to `memory`).
- Add a setter next to `SetMemory`:
  ```go
  // SetVocabSummary injects the WS routing vocabulary summary so the ToT
  // propose phase can author ws_* choreography from concrete type names.
  // Empty string is a no-op (non-WS projects get an unchanged prompt).
  func (t *ToTPlanner) SetVocabSummary(s string) { t.vocabSummary = s }
  ```
- In `buildProposeTask`, build a vocab block and insert it after the `Project Model:` block. Add before the `return fmt.Sprintf(...)`:
  ```go
  	vocabBlock := ""
  	if t.vocabSummary != "" {
  		vocabBlock = fmt.Sprintf("\nWS Routing Vocabulary:%s\n", t.vocabSummary)
  	}
  ```
  Then change the `return fmt.Sprintf(...)` template to include `%s` after the model summary and pass `vocabBlock`:
  ```go
  	return fmt.Sprintf(`Propose %d different test strategies.
  %sParent strategy: %s
  Project Model:
  %s
  %s
  Test Goal: %s

  Each strategy should focus on a different aspect (happy path, error handling, edge cases, security, etc.) and include concrete test case descriptions.`,
  		t.config.GenerateN, memoryBlock, parent.Description, modelSummary, vocabBlock, goal)
  ```
  (`renderVocabSummary` already begins with `\n\n## WS Routing Vocabulary ...`, so the `WS Routing Vocabulary:` label above is intentionally redundant-free — it reads as a section header followed by the grouped summary. If you prefer, drop the literal label and just inject `%s` directly; either is acceptable, pick the literal label for prompt readability.)

In `internal/head/scout/plan_phases.go`, in `executeDeepPlanning`, right after `planner.SetMemory(memory)`:
```go
		planner.SetVocabSummary(renderVocabSummary(s.config.Services))
```

- [ ] **Step 8: Add a ToT vocab-injection unit test**

Add to `internal/head/scout/vocab_context_test.go`:

```go
func TestToTProposeTaskIncludesVocab(t *testing.T) {
	planner := &ToTPlanner{config: ToTConfig{GenerateN: 3}}
	planner.SetVocabSummary("\n\n## WS Routing Vocabulary (realtime, 1 edges)\nbridge->web broadcast_web (1): workflow:task_progress\n")
	task := planner.buildProposeTask(PlanCandidate{Description: "seed"}, &project.ProjectModel{}, "cover relay")
	if !strings.Contains(task, "WS Routing Vocabulary") ||
		!strings.Contains(task, "workflow:task_progress") {
		t.Errorf("ToT propose task missing vocab summary\n--- task ---\n%s", task)
	}
}
```

Run: `go test ./internal/head/scout/ -run TestToTProposeTaskIncludesVocab -v`
Expected: PASS.

- [ ] **Step 9: Add the loader wiring (auto-load vocab alongside protocol)**

In `internal/project/loader.go`, inside `resolveProtocolRefs`, immediately after `svc.Protocol = &p` and before `svc.ProtocolRef = ""`, load the matching vocab file by the same ref name (best-effort; missing is not an error):

```go
		svc.Protocol = &p
		// Load the routing vocabulary alongside the protocol when a vocab file
		// of the same name exists. Vocab is optional: a missing file is not an
		// error (the service simply has no Vocabulary for Scout prompt context).
		vocabPath := filepath.Join(baseDir, ".cerberus", "vocab", svc.ProtocolRef+".vocab.yaml")
		if vdata, verr := os.ReadFile(vocabPath); verr == nil {
			var v Vocabulary
			if perr := yaml.Unmarshal(vdata, &v); perr != nil {
				return fmt.Errorf("services[%d]: vocab %q: parse: %w", i, svc.ProtocolRef, perr)
			}
			svc.Vocabulary = &v
		}
		svc.ProtocolRef = ""
```

(`os` and `filepath` and `yaml` are already imported in loader.go.)

- [ ] **Step 10: Add a loader test for vocab auto-load**

Add this test to `internal/project/loader_test.go` (same package, so it reuses the existing `writeProtocolFile` helper and matches the established minimal-protocol shape `framing: json\ntype_path: type\n` proven by the surrounding `TestLoadFromYAML*` tests — do NOT hand-roll a `roles`/`discriminator` block, which over-specifies and risks a `ValidateProtocol` failure):

```go
func TestResolveProtocolRefsLoadsVocab(t *testing.T) {
	dir := t.TempDir()
	writeProtocolFile(t, dir, "open-agents", "framing: json\ntype_path: type\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus", "vocab"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml"),
		[]byte("source:\n  protocol_ref: open-agents\nedges:\n  - from_role: bridge\n    to_role: web\n    type: session:created\n    trigger: message_handled\n    delivery: {mode: broadcast_web}\n    source: {spans: [{start: 1, end: 1}]}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "project.yaml"),
		[]byte("project:\n  name: t\nservices:\n  - name: rt\n    url: http://localhost:8787\n    protocol_ref: open-agents\n"), 0644))

	cfg, err := LoadFromFile(filepath.Join(dir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Vocabulary, "Vocabulary not loaded alongside protocol_ref")
	require.Len(t, cfg.Services[0].Vocabulary.Edges, 1)

	// A missing vocab file is not an error: a second service with a protocol
	// ref but no vocab file loads fine and leaves Vocabulary nil.
	writeProtocolFile(t, dir, "bare", "framing: json\ntype_path: type\n")
	// (covered implicitly by existing tests where no vocab file exists)
}
```

`loader_test.go` already imports `os`, `path/filepath`, `testify/require`, so no new imports are needed.

- [ ] **Step 11: Run the loader test + full scout/project unit suites**

Run: `go test ./internal/project/ ./internal/head/scout/ -v`
Expected: PASS.

- [ ] **Step 12: Run fmt + lint + full unit tests**

Run: `make fmt && make test`
Expected: clean (integration tests are `//go:build integration` and skipped by `make test`).

- [ ] **Step 13: Commit**

```bash
git add internal/project/schema.go internal/project/loader.go internal/project/loader_test.go internal/head/scout/vocab_context.go internal/head/scout/vocab_context_test.go internal/head/scout/direct_planning.go internal/head/scout/tot.go internal/head/scout/plan_phases.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit -m "feat(scout): load service Vocabulary and render routing summary into both planners"
```

---

## Acceptance

- `make check` clean.
- `go test ./internal/vocabextract/ -v` green (Tasks 1–4 fixtures).
- `go test ./internal/project/ ./internal/head/scout/ -v` green (Task 5 loader + render).
- `go test -tags integration -run TestVocabularyDriven ./internal/head/agent/ -v` green **and** free of the `from_role == web` workaround (session:send web→web relay is driven by `RouteField`).
- Task 5 proof that vocab enters planning context: `TestBuildPlanningContextIncludesVocab` (direct) and `TestToTProposeTaskIncludesVocab` (ToT) both green.
- The committed `open-agents.vocab.yaml` carries `route_field` + `on_missing_route` on session:send's web→web edge and `exclude_sender: true` on broadcast edges that pass `ws`.

## Self-review notes

- **Spec coverage (v1 spec §7.2):** Scout vocab wiring — the explicit v1 follow-up — is implemented by Task 5 (load + prompt render). Tasks 1–4 close fidelity gaps surfaced in the v1 plan's "Self-review notes" and the dynamic test's workaround comment.
- **Placeholder scan:** every code step shows the actual fixture/test/implementation; no TBD/TODO. Loader test minimal-protocol shape is called out for adjustment against `Protocol.Validate`.
- **Type consistency:** extractor JSON field names (`route_field`, `on_missing_route.{kind,code}`, `delivery.exclude_sender`, `best_effort`, `trigger` values `fetch_branch`/`disconnect_bridge`) match the existing `project.VocabEdge` / `VocabDelivery` / `VocabMissingRoute` tags and the assertion structs in `extract_test.go`. `renderVocabSummary` reads `project.VocabEdge` exported fields only.
- **Ordering hazard (Task 1):** extractor change → fixture green → regenerate dogfood vocab → edit integration test. Reversing the last two makes the integration test red because the committed vocab lacks the new `route_field`.
- **Scope (YAGNI):** no new `vocab_ref` config field (auto-load by `protocol_ref`); no deterministic `ws_cases.go` rewrite; both planners reuse the single `renderVocabSummary` helper (direct via `buildPlanningContext`, ToT via `SetVocabSummary` → `buildProposeTask`).
- **Non-WS projects:** `renderVocabSummary` returns "" and `resolveProtocolRefs` only loads vocab when a protocol_ref resolves, so non-vocab projects get byte-identical prompts and configs.

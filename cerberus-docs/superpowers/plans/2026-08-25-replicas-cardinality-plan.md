# Replicas Cardinality Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** One actor declaration expands to N real process instances, and claims with `implies_cardinality: N` are proven only when N distinct real actors are exercised by passing bound cases.

**Architecture:** Expansion at the single load choke point (`project.LoadFromYAML`); per-replica variance via the existing `{{actor.name}}` template (plus newly-templated capture_json dot-paths); role-declared claim bindings unioned into the two per-role generators; cardinality enforced in `ReconcileClaims` on a distinct-ACTOR basis.

**Tech Stack:** Go 1.25, pure stdlib + existing deps (yaml.v3, testify).

**Spec:** `cerberus-docs/superpowers/specs/2026-08-25-replicas-cardinality-design.md`

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NEVER Co-Authored-By.
- Comments and commit messages in English.
- Work on branch `feat/replicas-cardinality` (already created; spec committed).
- Docs only under `cerberus-docs/`.
- No CGo. Follow existing comment density/naming.

---

### Task 1: Actor.replicas expansion in project.LoadFromYAML

**Files:**
- Modify: `internal/project/schema.go` (Actor struct, after `Entry`/`Service` fields)
- Modify: `internal/project/loader.go` (LoadFromYAML, after `applyDefaults(&cfg)`; new `expandReplicas` func at file bottom)
- Test: `internal/project/replicas_test.go` (new)

**Interfaces:**
- Produces: `Actor.Replicas int` (`yaml:"replicas,omitempty"`); load-time invariant — after `LoadFromYAML`, every actor in `cfg.Actors` has `Replicas == 0` and a concrete unique `Name` (`base-1`…`base-N` for `replicas: N`). Later tasks rely on expanded names only.

- [ ] **Step 1: Write the failing test**

```go
package project

import "testing"

func TestExpandReplicas(t *testing.T) {
	yaml := `
project: {name: rt}
actors:
  - name: bridge-pty
    fidelity: real-process
    replicas: 3
    process:
      setup: ["./bin", "pair", "-n", "{{actor.name}}"]
      start: ["./bin", "start", "-d", "{{actor.name}}"]
`
	cfg, err := LoadFromYAML([]byte(yaml), "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Actors) != 3 {
		t.Fatalf("want 3 expanded actors, got %d", len(cfg.Actors))
	}
	want := []string{"bridge-pty-1", "bridge-pty-2", "bridge-pty-3"}
	for i, a := range cfg.Actors {
		if a.Name != want[i] {
			t.Fatalf("actor %d name = %q, want %q", i, a.Name, want[i])
		}
		if a.Replicas != 0 {
			t.Fatalf("expanded actor must carry Replicas 0, got %d", a.Replicas)
		}
	}
}

func TestExpandReplicasValidation(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"replicas on emulated actor", `
project: {name: rt}
actors:
  - name: a
    replicas: 2
`, "replicas requires fidelity real-process"},
		{"expanded name collision", `
project: {name: rt}
actors:
  - name: a
    fidelity: real-process
    replicas: 2
    process: {start: ["x"]}
  - name: a-1
    fidelity: real-process
    process: {start: ["x"]}
`, "duplicate actor name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadFromYAML([]byte(tc.yaml), "")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}
```

(import `strings` alongside `testing`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestExpandReplicas -v`
Expected: FAIL — `cfg.Actors` has 1 actor with Replicas=3 (field doesn't exist yet → compile error; that is the red).

- [ ] **Step 3: Implement**

In `internal/project/schema.go`, add to `Actor` (after the `Service` field):

```go
	// Replicas expands this actor into N identical real-process instances at
	// load time (names base-1..base-N). Per-instance variance is authored via
	// the {{actor.name}} template; only fidelity real-process actors may use
	// it. Absent/0 means exactly one actor (the declaration itself).
	Replicas int `yaml:"replicas,omitempty"`
```

In `internal/project/loader.go`, call it in `LoadFromYAML` right after `applyDefaults(&cfg)`:

```go
	applyDefaults(&cfg)
	if err := expandReplicas(&cfg); err != nil {
		return nil, err
	}
```

and add at the file bottom:

```go
// expandReplicas turns every actor with replicas: N into N actors named
// base-1..base-N (index from 1 so existing paired-device rows keyed by name
// stay valid). Per-instance fields carry {{actor.name}} and need no rewrite.
// Non-process actors and expanded-name collisions are load errors.
func expandReplicas(cfg *Config) error {
	seen := map[string]bool{}
	var out []Actor
	for i := range cfg.Actors {
		a := cfg.Actors[i]
		n := a.Replicas
		if n == 0 {
			n = 1
		}
		if a.Replicas > 0 && a.Fidelity != FidelityRealProcess {
			return fmt.Errorf("actors[%d] %q: replicas requires fidelity real-process", i, a.Name)
		}
		for j := 1; j <= n; j++ {
			clone := a
			clone.Replicas = 0
			if a.Replicas > 0 {
				clone.Name = fmt.Sprintf("%s-%d", a.Name, j)
			}
			if seen[clone.Name] {
				return fmt.Errorf("duplicate actor name after replicas expansion: %q", clone.Name)
			}
			seen[clone.Name] = true
			out = append(out, clone)
		}
	}
	cfg.Actors = out
	return nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/project/ -v`
Expected: PASS (all existing tests too — expansion is a no-op without `replicas`).

- [ ] **Step 5: Commit**

```bash
git add internal/project/schema.go internal/project/loader.go internal/project/replicas_test.go
git commit -m "feat(project): actor replicas expansion at load time"
```

---

### Task 2: capture_json dot-path templating

**Files:**
- Modify: `internal/session/harness.go:311` (capture func)
- Test: `internal/session/harness_test.go` (extend existing capture tests)

**Interfaces:**
- Consumes: existing `h.tmpl(spec.CaptureFile, actor)` helper (same context applies to dot-paths).
- Produces: `capture_json` values may carry `{{actor.name}}` etc.; resolved before `dotPathValue`.

- [ ] **Step 1: Write the failing test**

Find the existing capture test in `harness_test.go` (grep `capture_json` / `CaptureJSON`). Add one case where the dot-path contains `{{actor.name}}`, e.g. config JSON `{"devices":{"d-1":{"deviceId":"dev-1"}}}` with actor named `d-1` and:

```go
			CaptureJSON: map[string]string{"deviceId": "devices.{{actor.name}}.deviceId"},
```

assert captured `PathParams["deviceId"] == "dev-1"`. Mirror the fixture style of the neighboring tests (temp dir + capture file + `h.capture(actor)`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestCapture -v`
Expected: FAIL — dot-path `devices.{{actor.name}}.deviceId` walked literally, segment not found.

- [ ] **Step 3: Implement**

In `internal/session/harness.go` `capture()`, change the loop at :311:

```go
	for param, path := range spec.CaptureJSON {
		v, err := dotPathValue(doc, h.tmpl(path, actor))
```

(and adjust the error line below to keep naming the templated path; the `:314` errorf keeps using the same variable).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/harness.go internal/session/harness_test.go
git commit -m "feat(session): template capture_json dot-paths with actor context"
```

---

### Task 3: Role.Claims binding in the per-role generators

**Files:**
- Modify: `internal/project/protocol_schema.go` (ProtocolRole)
- Modify: `internal/head/scout/ws_cases.go:191` (realE2ECases struct literal)
- Modify: `internal/head/scout/real_responder_cases.go:69` (realResponderCases struct literal)
- Test: `internal/head/scout/role_claims_test.go` (new)

**Interfaces:**
- Produces: `ProtocolRole.Claims []string` (`yaml:"claims,omitempty"`); generated per-role cases carry the union `{ws-relay-messaging} ∪ role.Claims`, dedup, order-stable. Task 4's reconcile consumes only the case `Claims` field — no new coupling.
- Shared helper: `roleClaimBindings(role *project.ProtocolRole) []string` (defined in `ws_cases.go`, used by both generators).

- [ ] **Step 1: Write the failing test**

```go
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRoleClaimsUnion(t *testing.T) {
	// Order-stable dedup against the hardcoded relay claim.
	got := roleClaimBindings(&project.ProtocolRole{Claims: []string{"multi-device-orchestration"}})
	want := []string{"ws-relay-messaging", "multi-device-orchestration"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	dup := roleClaimBindings(&project.ProtocolRole{Claims: []string{"ws-relay-messaging", "x"}})
	if len(dup) != 2 || dup[0] != "ws-relay-messaging" || dup[1] != "x" {
		t.Fatalf("dedup failed: %v", dup)
	}
	if got := roleClaimBindings(nil); len(got) != 1 || got[0] != "ws-relay-messaging" {
		t.Fatalf("nil role must keep the relay claim only: %v", got)
	}
}

func TestRealE2ECasesCarryRoleClaims(t *testing.T) {
	// Minimal service with one real role declaring a claim; the generated
	// reale2e case must bind BOTH ids. Reuse the fixture style of
	// ws_cases_test.go's realE2E tests (grep "reale2e" there).
	svc := project.Service{Name: "rt", URL: "ws://localhost:1/ws", Protocol: reale2eProtocolFixture()}
	svc.Protocol.Roles["bridge"].Claims = []string{"multi-device-orchestration"}
	cases := realE2ECases(svc, map[string]bool{"bridge": true})
	if len(cases) == 0 {
		t.Fatal("expected generated cases")
	}
	for _, c := range cases {
		if !containsStr(c.Claims, "ws-relay-messaging") || !containsStr(c.Claims, "multi-device-orchestration") {
			t.Fatalf("case %s claims = %v", c.ID, c.Claims)
		}
	}
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
```

`reale2eProtocolFixture` — copy the smallest protocol fixture already used by `ws_cases_test.go`'s realE2E tests (grep `TestRealE2E` in that file); it needs a credentialed `web` role and a `bridge` role. Do not invent a new fixture shape.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run 'TestRoleClaimsUnion|TestRealE2ECasesCarryRoleClaims' -v`
Expected: FAIL — `Claims` field and `roleClaimBindings` undefined (compile error is the red).

- [ ] **Step 3: Implement**

`protocol_schema.go`, in `ProtocolRole` after `RequestPayload`:

```go
	// Claims lists ledger claim ids that cases derived for this role bind in
	// addition to the default relay claim. SUT fact: which promise a role's
	// per-role cases evidence lives here, not in scout code.
	Claims []string `yaml:"claims,omitempty"`
```

`ws_cases.go`, near `wsRelayClaimID` (:296):

```go
// roleClaimBindings returns the claims a per-role generated case binds: the
// relay claim first, then the role's declared claims, order-stable, deduped.
func roleClaimBindings(role *project.ProtocolRole) []string {
	out := []string{wsRelayClaimID}
	if role == nil {
		return out
	}
	for _, c := range role.Claims {
		if c != wsRelayClaimID {
			out = append(out, c)
		}
	}
	return out
}
```

Replace `Claims: []string{wsRelayClaimID},` at ws_cases.go:191 with `Claims: roleClaimBindings(svc.Protocol.Roles[roleName]),` and the same literal at real_responder_cases.go:69 with `Claims: roleClaimBindings(role),` (that loop already holds `role`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/head/scout/ -v`
Expected: PASS (all existing scout tests unchanged — roles without Claims bind exactly as before).

- [ ] **Step 5: Commit**

```bash
git add internal/project/protocol_schema.go internal/head/scout/ws_cases.go internal/head/scout/real_responder_cases.go internal/head/scout/role_claims_test.go
git commit -m "feat(scout): protocol roles declare claim bindings for per-role cases"
```

---

### Task 4: Cardinality enforcement in claims reconcile

**Files:**
- Modify: `internal/session/claims_gate.go` (collectRealIdentities → also build actor index; red-line string includes Reason)
- Modify: `internal/session/claims_reconcile.go` (ClaimVerdict.Reason; distinct-actor counting; ReconcileClaims signature)
- Test: `internal/session/claims_cardinality_test.go` (new)

**Interfaces:**
- Produces:
  - `type realActorIndex struct { Roles map[string]bool; RoleActor map[string]string; ActorIDs map[string][]string; ActorByValue map[string]string }` (Roles = today's realRoleActors; RoleActor = role name → actor name; ActorIDs = actor name → captured path-param values; ActorByValue = captured value → actor name)
  - `collectRealIdentities(cfg) realActorIndex` (return type changes; single caller `claims_gate.go:66` updates with it)
  - `ReconcileClaims(claims []project.Claim, results []agent.StepResult, idx realActorIndex) []ClaimVerdict`
  - `ClaimVerdict.Reason string` ("" unless a cardinality shortfall explains the status)

- [ ] **Step 1: Write the failing test**

```go
package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func cardinalityFixture() (realActorIndex, []agent.StepResult) {
	idx := realActorIndex{
		Roles:     map[string]bool{"bridge": true, "bridge2": true, "bridge3": true},
		RoleActor: map[string]string{"bridge": "b1", "bridge2": "b2", "bridge3": "b3"},
		ActorIDs: map[string][]string{
			"b1": {"dev-1"}, "b2": {"dev-2"}, "b3": {"dev-3"},
		},
		ActorByValue: map[string]string{"dev-1": "b1", "dev-2": "b2", "dev-3": "b3"},
	}
	// One passing real-tier case per replica, bound to the cardinality claim:
	// each body carries the {{role.deviceId}} placeholder for its role.
	mk := func(id, role string) agent.StepResult {
		return agent.StepResult{Status: agent.StepPassed, TestCase: &agent.TestCase{
			ID: id, Claims: []string{"multi"},
			Steps: []agent.TestStep{{Action: "ws_send", Message: `{"deviceId":"{{` + role + `.deviceId}}"`}},
		}}
	}
	return idx, []agent.StepResult{mk("c1", "bridge"), mk("c2", "bridge2"), mk("c3", "bridge3")}
}

func TestCardinalityProvenAtThree(t *testing.T) {
	idx, results := cardinalityFixture()
	claims := []project.Claim{{ID: "multi", Critical: true, ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if len(v) != 1 || v[0].Status != ClaimProven || v[0].Reason != "" {
		t.Fatalf("verdict = %+v, want proven without reason", v)
	}
}

func TestCardinalityShortfallIsEmulatedOnlyWithReason(t *testing.T) {
	idx, results := cardinalityFixture()
	results = results[:2] // only two replicas exercised
	claims := []project.Claim{{ID: "multi", Critical: true, ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimEmulatedOnly || v[0].Reason != "cardinality 2/3" {
		t.Fatalf("verdict = %+v, want emulated-only cardinality 2/3", v[0])
	}
}

func TestCardinalityCountsActorsNotRoles(t *testing.T) {
	// bridge and bridge2 backed by the SAME actor: two roles, one identity.
	idx, results := cardinalityFixture()
	idx.RoleActor["bridge2"] = "b1"
	idx.ActorIDs = map[string][]string{"b1": {"dev-1"}, "b3": {"dev-3"}}
	claims := []project.Claim{{ID: "multi", ImpliesCardinality: 2}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimEmulatedOnly {
		t.Fatalf("same-actor two-roles must count once: %+v", v[0])
	}
}

func TestCardinalityRawIdBodyMatchAttributesActor(t *testing.T) {
	idx, results := cardinalityFixture()
	results[2].TestCase.Steps[0].Message = `{"deviceId":"dev-3"}`
	claims := []project.Claim{{ID: "multi", ImpliesCardinality: 3}}
	v := ReconcileClaims(claims, results, idx)
	if v[0].Status != ClaimProven {
		t.Fatalf("raw-id match must credit the owning actor: %+v", v[0])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestCardinality -v`
Expected: FAIL — `realActorIndex` / `Reason` undefined (compile red).

- [ ] **Step 3: Implement**

`claims_gate.go`: change `collectRealIdentities` to build and return the index:

```go
type realActorIndex struct {
	// Roles: protocol role names bound to a real-process actor (today's
	// realRoleActors namespace — step Role/AuthRole carry role names).
	Roles map[string]bool
	// RoleActor: role name -> backing actor name.
	RoleActor map[string]string
	// ActorIDs: actor name -> captured path-param values (deviceId etc.).
	ActorIDs map[string][]string
}
```

Populate `RoleActor[name] = r.CredentialRef` where today's loop sets `realRoleActors[name] = true`; populate `ActorIDs[a.Name]` with the actor's PathParams values (keep a flat `realActorIds` local if `caseEvidenceTier` still needs it — see below).

`claims_reconcile.go`:
- Add `Reason string` to `ClaimVerdict`.
- Keep `caseEvidenceTier(tc, idx.Roles, flatIDs)` behavior; add:

```go
// caseRealActors attributes a PASSED case's evidence to the real actors it
// touched: role-bound steps and {{role.param}} placeholder bodies credit the
// role's backing actor; raw-id body matches credit the actor owning the
// captured value. Distinct actors (not roles, not raw values) are the
// cardinality basis — same actor behind two roles counts once.
func caseRealActors(tc agent.TestCase, idx realActorIndex) map[string]bool {
	actors := map[string]bool{}
	for _, s := range tc.Steps {
		for _, role := range []string{s.Role, s.AuthRole} {
			if role != "" {
				if a, ok := idx.RoleActor[role]; ok {
					actors[a] = true
				}
			}
		}
	}
	for _, body := range rawSendBodies(tc) {
		for role, a := range idx.RoleActor {
			if role != "" && strings.Contains(body, "{{"+role+".") {
				actors[a] = true
			}
		}
		for id, a := range idx.ActorByValue {
			if id != "" && strings.Contains(body, id) {
				actors[a] = true
			}
		}
	}
	return actors
}
```

- In `ReconcileClaims`, after the passing-case loop, for claims with `ImpliesCardinality > 0` (derive the flat id list `caseEvidenceTier` needs from `idx.ActorIDs` values, keeping today's tier logic untouched):

```go
		if c.ImpliesCardinality > 0 && v.Status == ClaimProven {
			distinct := map[string]bool{}
			for _, r := range results {
				if r.TestCase == nil || !claimsBound(r.TestCase.Claims, c.ID) || r.Status != agent.StepPassed {
					continue
				}
				for a := range caseRealActors(*r.TestCase, idx) {
					distinct[a] = true
				}
			}
			if len(distinct) < c.ImpliesCardinality {
				v.Status = ClaimEmulatedOnly
				v.Reason = fmt.Sprintf("cardinality %d/%d", len(distinct), c.ImpliesCardinality)
			}
		}
```

- Update `reconcileClaimsInto` (:66-67) to the new signature, and the red-line entry (:93) to append ` (reason: %s)` when `v.Reason != ""`.
- Update any other ReconcileClaims callers/tests to the new signature (grep `ReconcileClaims(`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/session/ -v`
Expected: PASS, including the existing reconcile tests (update their call sites to the index shape; their verdict expectations must not change).

- [ ] **Step 5: Commit**

```bash
git add internal/session/claims_gate.go internal/session/claims_reconcile.go internal/session/claims_cardinality_test.go
git commit -m "feat(session): enforce implies_cardinality on distinct real actors"
```

---

### Task 5: Dogfood realtime-e2e at N=3

**Files:**
- Modify: `dogfood/realtime-e2e/.cerberus/project.yaml` (actors + settings)
- Modify: `dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml` (bridge3 role; claims on bridge roles)
- Modify: `dogfood/realtime-e2e/.cerberus/claims.yaml` (new claim)
- Test: `internal/project/dogfood_config_test.go` (new — loads the real dogfood config)

**Interfaces:**
- Consumes: Tasks 1-4 (Replicas expansion, capture_json templating, Role.Claims, enforcement).
- Produces: the live-run configuration the final task validates.

- [ ] **Step 1: Write the failing config test**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project/ -run TestDogfoodRealtimeE2EReplicas -v`
Expected: FAIL — bridge-pty-3 missing.

- [ ] **Step 3: Rewrite the dogfood config**

`project.yaml` — replace the two `- name: bridge-pty-1` / `- name: bridge-pty-2` blocks with ONE:

```yaml
  - name: bridge-pty
    credentials: {}
    fidelity: real-process
    replicas: 3
    process:
      workdir: "../../../open-agents/bridge"
      setup: ["./build/open-agents-bridge", "pair", "--dev", "--server", "http://localhost:8989", "-n", "{{actor.name}}"]
      start: ["./build/open-agents-bridge", "start", "-d", "{{actor.name}}"]
      env:
        HOME: "{{runtime.dir}}/{{actor.name}}-home"   # isolate .open-agents-bridge/config.json per instance
        PATH: "{{runtime.dir}}/../../shim:{{env.PATH}}"   # deterministic claude shim first: zero-LLM claude-pty sessions
      capture_file: "{{runtime.dir}}/{{actor.name}}-home/.open-agents-bridge/config.json"
      capture_json:
        deviceId: "devices.{{actor.name}}.deviceId"
      ready_pattern: "Connected to server successfully"
      ready_timeout: 30s
```

(names expand to the bit-identical bridge-pty-1/2 plus new -3; D1 pairing rows stay valid). Keep the comment block above; extend it with: expanded via replicas, index from 1.

`settings.ai_budget.session_total_tokens`: `700000` → `800000` (update the adjacent comment: run 15 spent ~711K; bridge3 adds ~15 judged cases).

`protocols/open-agents.yaml` — add `claims: [ws-relay-messaging, multi-device-orchestration]` under `bridge`, `bridge2`, and append a `bridge3` role block copied from `bridge2` with `credential_ref: bridge-pty-3` and every `bridge2` template replaced by `bridge3` (including `deviceId: "{{bridge3.deviceId}}"` and the `session:start` request_payload entry).

`claims.yaml` — add after `schedule-real-cli`:

```yaml
  - id: multi-device-orchestration
    text: "多设备编排：N=3 个真实 bridge 设备各自被真实会话独立寻址（跨设备 mission fan-out 为后续项）"
    critical: true
    implies_cardinality: 3
```

and update the header comment (currently says multi-device-orchestration stays in ws-realtime's ledger — now evidenced here at N=3).

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/project/ -v && go build ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add dogfood/realtime-e2e/.cerberus/project.yaml dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml dogfood/realtime-e2e/.cerberus/claims.yaml internal/project/dogfood_config_test.go
git commit -m "feat(dogfood): realtime-e2e at 3 bridge replicas with cardinality claim"
```

(Note: do NOT `git add` `dogfood/realtime-e2e/.cerberus/findings.yaml` — runtime drift.)

---

### Task 6: Verification ladder + docs + merge

**Files:**
- Create: `cerberus-docs/technical/dogfood/2026-08-25-run16-replicas-cardinality.md`
- No code changes.

- [ ] **Step 1: `make check`**

Run: `make check`
Expected: fmt+lint+test all green.

- [ ] **Step 2: `make integration-openagents`**

Run: `make integration-openagents`
Expected: exit 0. (`TestRelayCoverage_ProbeTriggerable` is a known 1-in-3 full-suite flake — on failure rerun the suite once before investigating; solo `-run` pass + suite pass = flake.)

- [ ] **Step 3: Live dogfood run**

```bash
cd /home/mason/Documents/code_projects/private/open-agents/apps/api && eval "$(fnm env)" && fnm use 22 && npm run dev   # background, wait for :8989 /health 200
cd /home/mason/Documents/code_projects/private/cerberus/dogfood/realtime-e2e && source ../../scripts/dogfood-realtime-e2e-env.sh && ../../build/cerberus run
```

(Never pipe the `source`. The env script rebuilds both binaries from working trees — both repos on intended branches.)

Expected, ALL of:
- exit 0 (NOT 3 — the new critical claim must prove)
- summary `Real actors: bridge-pty-1, bridge-pty-2, bridge-pty-3`
- claims line `2 proven / 0 emulated-only / 0 unevidenced / 1 wont-test`
- coverage `reached:true, gaps:0`
- bridge3 cases present and passing (`grep 'bridge3'` on run output: reale2e + realresp cases)

- [ ] **Step 4: Run doc + memory**

Write `cerberus-docs/technical/dogfood/2026-08-25-run16-replicas-cardinality.md` (numbers from the run, expected-vs-observed for every line above; any flake noted). Update memory: new fact file + MEMORY.md line; update `gap-burndown-complete` pointer if relevant.

- [ ] **Step 5: Merge and push (end-of-branch review first)**

Self-review the full branch diff (`git diff main..feat/replicas-cardinality`), then:

```bash
git checkout main && git merge --ff-only feat/replicas-cardinality && git push origin main
```

Commit author `binoctal <binoctal@gmail.com>` on every commit; no Co-Authored-By.

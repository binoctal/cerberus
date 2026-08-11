# HTTP-Push Coverage Attribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make path coverage credit HTTP-triggered server-push edges so the `http_request` feature's push capability is reflected in `coverage_pct`.

**Architecture:** `requiredEdges` (`internal/session/coverage.go`) gains a second source — it synthesizes one `VocabEdge{FromRole:"", ToRole, Type, Trigger:"http_trigger"}` per declared `Protocol.HTTPTriggers`. The existing receive-driven attribution (`exercisedEdges`) is already FromRole-agnostic and credits the synthesized edge when its recipient receives the message. A display helper renders the empty `FromRole` as `server` in gap details.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), module `github.com/binoctal/cerberus`.

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- No CGo. Comments and commit messages in English.
- Zero regression: a service without `http_triggers` produces byte-identical `requiredEdges` output and identical coverage; the receive-driven attribution rule is unchanged.
- Tests: `go test ./internal/session/`. Live: `make integration-openagents` or autonomous `cerberus run`.
- Documentation only in `cerberus-docs/`, never `docs/`.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-11-http-push-coverage-attribution-design.md`

---

### Task 1: Synthesize HTTP-push edges + render empty origin

**Files:**
- Modify: `internal/session/coverage.go` — `requiredEdges` (≈line 271), gap-detail render (≈line 66), new `originLabel` helper.
- Test: `internal/session/coverage_test.go`

**Interfaces:**
- Consumes: `project.VocabEdge` (fields `FromRole`, `ToRole`, `Type`, `Trigger`), `svc.Protocol.HTTPTriggers` (`HTTPTrigger.Effect.{ToRole,MessageType}`) — both already in the codebase.
- Produces: `requiredEdges` now returns vocab edges + synthesized http_trigger edges; `originLabel(e) string` helper.

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/coverage_test.go` (package `session`). Mirror the existing `TestExercisedEdges_PushProtocolReceiveDriven` construction for `StepResult`/`Evidence`/`TestCase` shapes.

```go
func TestRequiredEdges_HTTPTriggers(t *testing.T) {
	sess := &Session{Config: project.Config{Services: []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
		}},
		Protocol: &project.Protocol{HTTPTriggers: []*project.HTTPTrigger{{
			ID:     "device-restart",
			Effect: project.HTTPTriggerEffect{MessageType: "device:restart", ToRole: "web"},
		}}},
	}}}}

	got := requiredEdges(sess)
	// One WS vocab edge + one synthesized http_trigger edge.
	if len(got) != 2 {
		t.Fatalf("requiredEdges returned %d edges, want 2: %+v", len(got), got)
	}
	var synth *project.VocabEdge
	for i := range got {
		if got[i].Trigger == "http_trigger" {
			synth = &got[i]
		}
	}
	if synth == nil {
		t.Fatalf("no synthesized http_trigger edge in %+v", got)
	}
	if synth.FromRole != "" || synth.ToRole != "web" || synth.Type != "device:restart" {
		t.Fatalf("synthesized edge = %+v, want {FromRole:\"\" ToRole:\"web\" Type:\"device:restart\"}", *synth)
	}
}

func TestRequiredEdges_NoHTTPTriggers_ZeroRegression(t *testing.T) {
	sess := &Session{Config: project.Config{Services: []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{
			{FromRole: "bridge", ToRole: "web", Type: "device:online", Trigger: "message_handled"},
			{FromRole: "web", ToRole: "bridge", Type: "session:send", Trigger: "connect_web"}, // filtered out
		}},
		Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}},
	}}}}
	got := requiredEdges(sess)
	if len(got) != 1 || got[0].Type != "device:online" {
		t.Fatalf("no http_triggers ⇒ only the message_handled vocab edge, got %+v", got)
	}
}

func TestExercisedEdges_HTTPTriggerCredit(t *testing.T) {
	required := []project.VocabEdge{
		{FromRole: "", ToRole: "web", Type: "device:restart", Trigger: "http_trigger"},
		// Same (ToRole,Type) but non-empty FromRole must NOT collide (distinct edgeKey).
		{FromRole: "bridge", ToRole: "web", Type: "device:restart", Trigger: "message_handled"},
	}
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: "c-web", Role: "web"},
		}},
		Evidence: []agent.Evidence{{
			Action: "ws_receive", ConnectionID: "c-web", MatchedType: "device:restart", Matched: true,
		}},
	}}
	exercised, _ := exercisedEdges(results, required)
	if !exercised["|web|device:restart"] {
		t.Fatal("synthesized http_trigger edge must be credited by the web receive")
	}
	if !exercised["bridge|web|device:restart"] {
		t.Fatal("the same-recipient WS edge must also be credited (distinct key, no collision)")
	}
	m := pathCoverage(results, required)
	if !m.Known || m.Pct != 1.0 {
		t.Fatalf("pathCoverage = %+v, want Known=true Pct=1.0", m)
	}
}

func TestGapRender_HTTPTriggerOrigin(t *testing.T) {
	e := project.VocabEdge{FromRole: "", ToRole: "web", Type: "device:restart", Trigger: "http_trigger"}
	got := fmt.Sprintf("edge %s→%s %s not exercised", originLabel(e), e.ToRole, e.Type)
	want := "edge server→web device:restart not exercised"
	if got != want {
		t.Fatalf("gap render = %q, want %q", got, want)
	}
	// A regular edge still renders its real FromRole.
	e2 := project.VocabEdge{FromRole: "bridge", ToRole: "web", Type: "device:online"}
	got2 := fmt.Sprintf("edge %s→%s %s not exercised", originLabel(e2), e2.ToRole, e2.Type)
	if got2 != "edge bridge→web device:online not exercised" {
		t.Fatalf("non-empty FromRole render = %q", got2)
	}
}
```

(If the `Session.Config`/`Service`/`Vocabulary`/`Protocol`/`HTTPTrigger` struct literals don't match the real field names in `internal/project/schema.go` / `vocabulary.go` / `protocol_schema.go`, adjust the literals to match — do not invent fields. `agent.Evidence` fields used: `Action`, `ConnectionID`, `MatchedType`, `Matched`, `ExpectAbsent`. `agent.TestStep` fields: `Action`, `ConnectionID`, `Role`. Confirm against `internal/head/agent/types.go`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run "TestRequiredEdges_HTTPTriggers|TestRequiredEdges_NoHTTPTriggers|TestExercisedEdges_HTTPTriggerCredit|TestGapRender_HTTPTriggerOrigin" ./internal/session/`
Expected: FAIL — `originLabel` undefined and/or `requiredEdges` doesn't synthesize.

- [ ] **Step 3: Implement the changes in `internal/session/coverage.go`**

Add the helper near `edgeKey`:

```go
// originLabel returns the edge's origin role for gap-detail display. A
// synthesized HTTP-triggered server-push edge has no sender role (empty
// FromRole); render it as "server" instead of an empty segment.
func originLabel(e project.VocabEdge) string {
	if e.FromRole == "" {
		return "server"
	}
	return e.FromRole
}
```

Update the gap-detail call site (≈line 66) from:
```go
					Detail: fmt.Sprintf("edge %s→%s %s not exercised", e.FromRole, e.ToRole, e.Type),
```
to:
```go
					Detail: fmt.Sprintf("edge %s→%s %s not exercised", originLabel(e), e.ToRole, e.Type),
```

Replace `requiredEdges` (≈line 271) with a version that reads both sources (drop the `continue` so `Protocol` is read even when `Vocabulary` is nil):

```go
// requiredEdges collects the declared required surface for path coverage:
// message_handled vocab edges (neither Unsupported nor Partial) PLUS one
// synthesized edge per declared http_trigger (an HTTP-triggered server push,
// modeled with an empty FromRole and Trigger="http_trigger"). Both are
// credited by the receive-driven exercisedEdges rule.
func requiredEdges(sess *Session) []project.VocabEdge {
	var out []project.VocabEdge
	for _, svc := range sess.Config.Services {
		if svc.Vocabulary != nil {
			for _, e := range svc.Vocabulary.Edges {
				if e.Trigger == "message_handled" && !e.Unsupported && !e.Partial {
					out = append(out, e)
				}
			}
		}
		// HTTP-triggered server-push edges: synthesize one required edge per
		// declared http_trigger so receive-driven attribution credits the push
		// when its recipient receives the message. Empty FromRole = system
		// origin; Trigger="http_trigger" distinguishes these from WS-relayed
		// vocab edges in gap output. (validateProtocolHTTPTriggers guarantees
		// ToRole/MessageType are non-empty and reference declared roles.)
		if svc.Protocol != nil {
			for _, tr := range svc.Protocol.HTTPTriggers {
				out = append(out, project.VocabEdge{
					FromRole: "",
					ToRole:   tr.Effect.ToRole,
					Type:     tr.Effect.MessageType,
					Trigger:  "http_trigger",
				})
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/session/`
Expected: PASS — the 4 new tests pass AND `TestExercisedEdges_PushProtocolReceiveDriven` (the locked attribution rule) still passes unchanged.

- [ ] **Step 5: Build the whole repo**

Run: `go build ./...`
Expected: success.

- [ ] **Step 6: Commit**

```bash
git add internal/session/coverage.go internal/session/coverage_test.go
git commit -m "feat(coverage): credit http-triggered server-push edges in path coverage"
```

---

### Task 2: Autonomous live verification + report

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` (append a section)

- [ ] **Step 1: Build and run autonomously**

Ensure open-agents is up on :8989 (`curl -s -o /dev/null -w "%{http_code}" http://localhost:8989/` should be non-000; if down, start with `setsid bash -c "cd ../open-agents/apps/api && eval \"\$(fnm env --shell bash)\" && fnm use 22 && exec npm run dev" >/tmp/oa.log 2>&1 &` and poll up to ~60s). Then:

```
make build
./build/cerberus run --config dogfood/ws-realtime/.cerberus/project.yaml \
  --dir dogfood/ws-realtime \
  --goal "Trigger a device restart over HTTP and observe the push over the realtime WS service"
```

Capture VERBATIM the `coverage assessment` line (`reached`/`gaps`/`coverage_pct`) and the `device-restart` case's `test case completed` + Examiner `verdict` lines. The expected honest outcome: the denominator grows by 1 (63→64) and, if the `device-restart` case runs and its web receive matches, coverage rises from 3/63 (≈0.0476) to **4/64 (0.0625)**. If the case does not run, coverage is 3/64 (≈0.0469) — also honest. Record whichever actually happened.

- [ ] **Step 2: Append the verification section**

Append to `cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md` a dated "2026-08-11 — HTTP-push coverage attribution" section: run setup (branch tip commit, server), the verbatim log lines, the honest before/after coverage numbers, and a one-line note that the `device-restart` edge is now a synthesized `http_trigger` required edge (FromRole empty, rendered `server→web`). Do NOT claim coverage rose if the log shows it did not.

- [ ] **Step 3: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-07-openagents-integration-test-report.md
git commit -m "docs(validation): http-push coverage attribution autonomous verification"
```

---

## Self-Review Notes

- **Spec coverage:** D1 (synthesize, no filter change) → Task 1 `requiredEdges`; D2 (empty FromRole + "server" render) → Task 1 `originLabel` + gap call site; D3 (scope boundary: requires existing vocab) → documented, no code (the dogfood has vocab); success criteria 1–3 → Task 1 tests; criterion 4 → Task 2; criterion 5 → Task 1 zero-regression test.
- **Type consistency:** synthesized `VocabEdge{FromRole:"", ToRole:tr.Effect.ToRole, Type:tr.Effect.MessageType, Trigger:"http_trigger"}` matches the `edgeKey`/`byToType` consumers (FromRole-agnostic); `originLabel` takes `project.VocabEdge` and is used at the one gap-detail site.
- **No placeholders:** struct-literal caveat is tied to confirming against the real schema files; all code blocks are complete.

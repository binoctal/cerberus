# Workflow Orchestration Coverage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Cover the 17 `workflow:*` WS edge gaps plus `web→web session:send` in the realtime-e2e dogfood via a mission-seeded case family and a web-role send family, lifting coverage 93.9% → ~98.5%.

**Architecture:** Two new scout generators (`missionSendCases`, `missionSeedCases`) attached in `WSCasesCovered` next to `httpRouteCases`/`realResponderCases`, one small executor addition (HTTP response capture + `{{case.*}}` substitution in `runSteps`), and data-only vocab marks for the two dead wire types. Attribution uses existing mechanisms unchanged.

**Tech Stack:** Go 1.25, `modernc.org/sqlite`, existing multi-executor (`internal/head/agent`), scout generators (`internal/head/scout`).

**Spec:** `cerberus-docs/superpowers/specs/2026-08-18-workflow-orchestration-coverage-design.md` — all file:line facts cited below are verified there; read it first.

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By lines.
- Code comments and commit messages in English; answers to the user in 简体中文.
- No CGo; `make check` (fmt + lint + test) green before every commit.
- Work on branch `feat/workflow-orchestration-coverage` (cross-repo dogfood → feature branch, NOT a worktree).
- Dogfood files live under `dogfood/realtime-e2e/.cerberus/` (cerberus repo) and `apps/api` + `.dev.vars` (sibling open-agents repo at `../open-agents`).
- Never include `max_concurrent_tasks` in a seeded plan payload (spec §2: the 0-trap stalls dispatch).
- The mission case must NOT bind the `schedule-real-cli` claim (spec §13).

---

### Task 1: Vocab marks for dead wire types + known-issue #5

**Files:**
- Modify: `dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml` (edges `workflow:job_completed`, `workflow:task_status_update`)
- Modify: `cerberus-docs/technical/dogfood/2026-08-16-openagents-known-issues.md` (new issue #5)
- Test: `internal/head/scout/ws_cases_test.go` (append one test)

**Interfaces:**
- Consumes: `VocabEdge.Partial/Unsupported` fields (`internal/project/vocabulary.go:52-53`); `requiredEdges` excludes marked edges (`internal/session/coverage.go:365`).
- Produces: vocab data later tasks rely on (denominator shrink by 2).

- [ ] **Step 1: Write the failing test** — append to `internal/head/scout/ws_cases_test.go`:

```go
// TestWorkflowDeadTypesMarked: the two dead wire types (no emitter in
// apps/api or bridge — spec §9) must carry unsupported marks so
// requiredEdges drops them from the denominator.
func TestWorkflowDeadTypesMarked(t *testing.T) {
	vocab := loadDogfoodVocab(t) // existing helper reading the dogfood vocab fixture; if none exists, parse dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml via os.ReadFile relative to ../../../
	for _, typ := range []string{"workflow:job_completed", "workflow:task_status_update"} {
		e := findVocabEdgeByType(t, vocab, "web", "bridge", typ) // helper: linear scan over vocab edges
		if !e.Unsupported {
			t.Errorf("%s must be marked unsupported (dead type, DO-drop family)", typ)
		}
	}
}
```

If `loadDogfoodVocab`/`findVocabEdgeByType` helpers do not exist, write them in this test file (read the yaml with `project.LoadVocabulary` or the loader the vocab validation helpers in `vocab_validation_helpers_test.go` already use — reuse, don't reinvent).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestWorkflowDeadTypesMarked -v`
Expected: FAIL — edges unmarked.

- [ ] **Step 3: Mark the edges** — in the vocab yaml, on both edges add:

```yaml
    unsupported: true
    notes: >-
      Dead type: whitelisted in room.ts but no emitter in apps/api or bridge
      (verified 2026-08-18). Completion is signalled by workflow:job_status /
      workflow:state_updated, broadcast DO-side (known issue #5, DO-drop family).
```

- [ ] **Step 4: Add known-issue #5** to `2026-08-16-openagents-known-issues.md`, same format as #1–#4: title "`workflow:job_completed` / `workflow:task_status_update` have no emitter", the room.ts:375/381 whitelist refs, the orchestrator.ts:949/1073 actual signals, and the cerberus consequence (vocab marks; success criteria use `job_status` out-of-band).

- [ ] **Step 5: Run test + make check, then commit**

```bash
go test ./internal/head/scout/ -run TestWorkflowDeadTypesMarked -v
make check
git add dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml cerberus-docs/technical/dogfood/2026-08-16-openagents-known-issues.md internal/head/scout/ws_cases_test.go
git commit -m "feat(vocab): mark workflow dead types unsupported + known-issue #5"
```

---

### Task 2: HTTP response capture + `{{case.*}}` substitution

**Files:**
- Modify: `internal/head/agent/types.go` (TestStep: new `Capture` field)
- Modify: `internal/head/agent/execute_phases_steps.go:291` (`runSteps`)
- Create: `internal/head/agent/case_params.go` (capture parsing + substitution helpers)
- Test: `internal/head/agent/execute_phases_steps_test.go` (append), `internal/head/agent/case_params_test.go`

**Interfaces:**
- Produces: `TestStep.Capture map[string]string` (`json:"capture,omitempty"`) — dot-path into the http_request JSON response body → per-case param name; helpers `substituteCaseParams(s TestStep, params map[string]string) TestStep` (rewrites `{{case.<name>}}` in `Body`/`URL`/`Message`) and `captureFromHTTPBody(body string, capture map[string]string) (map[string]string, error)` (dot-path walk, `map[string]any` intermediates, scalars stringified via `fmt.Sprint`, missing path ⇒ error). Params live in `se.caseParams map[string]string` on `stepExecution` (`internal/head/agent/execute_phases_types.go:11`).
- Consumes: `types.HTTPResult.Body` (`internal/types/result_http.go:12`).

- [ ] **Step 1: Write the failing tests**

`internal/head/agent/case_params_test.go`:

```go
package agent

import (
	"strings"
	"testing"
)

func TestCaptureFromHTTPBody(t *testing.T) {
	got, err := captureFromHTTPBody(`{"plan":{"id":"plan_123"},"n":7}`,
		map[string]string{"plan.id": "planId", "n": "count"})
	if err != nil {
		t.Fatal(err)
	}
	if got["planId"] != "plan_123" || got["count"] != "7" {
		t.Fatalf("got %v", got)
	}
	if _, err := captureFromHTTPBody(`{}`, map[string]string{"plan.id": "planId"}); err == nil {
		t.Fatal("missing path must be a hard error")
	}
	if _, err := captureFromHTTPBody(`not json`, map[string]string{"a": "a"}); err == nil {
		t.Fatal("unparseable body must be a hard error")
	}
}

func TestSubstituteCaseParams(t *testing.T) {
	s := TestStep{URL: "http://x/api/users/{{case.planId}}", Body: `{"plan":"{{case.planId}}"}`, Message: `{"id":"{{case.planId}}"}`}
	out := substituteCaseParams(s, map[string]string{"planId": "plan_9"})
	if !strings.Contains(out.URL, "plan_9") || !strings.Contains(out.Body, "plan_9") || !strings.Contains(out.Message, "plan_9") {
		t.Fatalf("unsubstituted: %+v", out)
	}
}
```

End-to-end test appended to `execute_phases_steps_test.go` (mirror `TestRunStepsCrossEndpoint`'s harness use — `newStepExecution`, echo servers):

```go
// TestRunStepsHTTPCaptureChain: a first http_request captures a nested id,
// a second request consumes it in URL and body.
func TestRunStepsHTTPCaptureChain(t *testing.T) {
	var secondBody string
	srv := newHTTPTestServer(t, func(w http.ResponseWriter, r *http.Request) { // helper: plain net/http server; add if absent
		if r.URL.Path == "/one" {
			_, _ = w.Write([]byte(`{"plan":{"id":"plan_abc"}}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		secondBody = string(b)
		_, _ = w.Write([]byte(`{}`))
	})
	tc := &TestCase{
		ID: "tc-http-capture", Target: srv.URL, Action: "ws_flow",
		Steps: []TestStep{
			{Action: "http_request", URL: srv.URL + "/one", Method: "GET",
				Capture: map[string]string{"plan.id": "planId"}},
			{Action: "http_request", URL: srv.URL + "/two/{{case.planId}}", Method: "POST",
				Body: `{"plan":"{{case.planId}}"}`, ExpectStatusClass: "2xx"},
		},
	}
	se := newStepExecution(t, tc)
	result := se.runSteps()
	if result.Status != StepPassed {
		t.Fatalf("status %s evidence %+v", result.Status, result.Evidence)
	}
	if !strings.Contains(secondBody, "plan_abc") {
		t.Fatalf("captured value not forwarded: %q", secondBody)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/agent/ -run 'TestCaptureFromHTTPBody|TestSubstituteCaseParams|TestRunStepsHTTPCaptureChain' -v`
Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement** — `internal/head/agent/case_params.go`:

```go
package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// caseParamRe matches {{case.<name>}} placeholders in step URL/Body/Message.
var caseParamRe = regexp.MustCompile(`\{\{case\.([A-Za-z0-9_]+)\}\}`)

// substituteCaseParams rewrites {{case.<name>}} placeholders from params.
// Leftover placeholders stay verbatim — the downstream request failing on a
// literal {{case.x}} is a clearer failure than a silent empty string.
func substituteCaseParams(s TestStep, params map[string]string) TestStep {
	replace := func(in string) string {
		return caseParamRe.ReplaceAllStringFunc(in, func(m string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(m, "{{case."), "}}")
			if v, ok := params[name]; ok {
				return v
			}
			return m
		})
	}
	s.URL, s.Body, s.Message = replace(s.URL), replace(s.Body), replace(s.Message)
	return s
}

// captureFromHTTPBody walks dot-paths into the JSON body and stringifies
// the scalar leaf. Missing paths are hard errors (clear failure over a
// silently-wrong later request — same policy as resolveURLParams).
func captureFromHTTPBody(body string, capture map[string]string) (map[string]string, error) {
	if len(capture) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("capture: response body is not JSON: %w", err)
	}
	out := make(map[string]string, len(capture))
	for path, name := range capture {
		cur := root
		var leaf any = cur
		ok := true
		for _, seg := range strings.Split(path, ".") {
			m, isMap := cur.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			leaf, ok = m[seg]
			if !ok {
				break
			}
			cur = leaf
		}
		if !ok {
			return nil, fmt.Errorf("capture: path %q not found in response", path)
		}
		switch leaf.(type) {
		case map[string]any, []any:
			return nil, fmt.Errorf("capture: path %q is not a scalar", path)
		}
		out[name] = fmt.Sprint(leaf)
	}
	return out, nil
}
```

TestStep addition (`types.go`, inside the http_request field block):

```go
	// http_request: response capture. Dot-path into the JSON response body
	// -> per-case param name, substitutable in later steps as {{case.<name>}}
	// (http_request URL/Body, ws_send Message).
	Capture map[string]string `json:"capture,omitempty"`
```

`runSteps` changes (`execute_phases_steps.go`, inside `for _, s := range se.tc.Steps`):
1. Add `se.caseParams` (map on `stepExecution`, initialize in `newStepExecutionWithIdx`).
2. First line of the loop body: `s = substituteCaseParams(s, se.caseParams)`.
3. Refactor the two http gate branches (`ExpectStatus` / `ExpectStatusClass`) into `httpStepPassed(s TestStep, result types.ExecutorResult) bool` so there is ONE pass point; after the pass point, for `s.Action == "http_request"`:

```go
		if len(s.Capture) > 0 {
			if hr, ok := result.(types.HTTPResult); ok {
				captured, err := captureFromHTTPBody(hr.Body, s.Capture)
				if err != nil {
					return se.failureResult(err, 1)
				}
				maps.Copy(se.caseParams, captured)
			}
		}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/agent/ -run 'TestCapture|TestSubstitute|TestRunStepsHTTPCaptureChain' -v`
Expected: PASS. Then `go test ./internal/head/agent/ -v` — no regression in existing step tests.

- [ ] **Step 5: Commit**

```bash
make check
git add internal/head/agent/types.go internal/head/agent/case_params.go internal/head/agent/case_params_test.go internal/head/agent/execute_phases_steps.go internal/head/agent/execute_phases_steps_test.go internal/head/agent/execute_phases_types.go
git commit -m "feat(executor): http_request response capture + {{case.*}} step substitution"
```

---

### Task 3: `missionSendCases` — web→bridge sends + session:send

**Files:**
- Create: `internal/head/scout/mission_send_cases.go`
- Modify: `internal/head/scout/ws_cases.go:63` (attach after `realResponderCases`)
- Test: `internal/head/scout/mission_send_cases_test.go`

**Interfaces:**
- Consumes: `realProcessRoles(cfg project.Config) map[string]bool` (ws_cases.go:76); `agent.TestStep` incl. Task 2's `Capture`; role param `{{bridge.deviceId}}` resolution (realResponderCases precedent, line 55).
- Produces: `missionSendCases(svc project.Service, realRoles map[string]bool) []agent.TestCase` — called from `WSCasesCovered`.

- [ ] **Step 1: Write the failing test**

```go
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

// One service with web + bridge roles (bridge real), workflow vocab edges.
func missionSendFixture() project.Service {
	return project.Service{
		Name: "open-agents", URL: "ws://localhost:8989/ws",
		Protocol: &project.Protocol{
			TypePath: "type",
			Roles: map[string]*project.ProtocolRole{
				"web":    {Params: map[string]string{"type": "web"}},
				"bridge": {Params: map[string]string{"type": "bridge"}},
			},
		},
		Vocabulary: workflowVocabFixture(), // edges: web->bridge workflow:start/pause/cancel/start_task + web->web session:send
	}
}

func TestMissionSendCases_Assembly(t *testing.T) {
	cases := missionSendCases(missionSendFixture(), map[string]bool{"bridge": true})
	byID := caseByID(cases) // helper: map[ID]TestCase; add locally if absent
	// start_task: send + hard receive of the deterministic echo.
	c := byID["open-agents-wf-start-task"]
	if c == nil {
		t.Fatal("start_task case missing")
	}
	if !hasStep(c, "ws_receive", "workflow:task_started") {
		t.Fatal("start_task must hard-receive workflow:task_started")
	}
	// pause: send-only, NO receive (bridge only logs — spec §8).
	p := byID["open-agents-wf-pause"]
	if p == nil || hasStep(p, "ws_receive", "") {
		t.Fatal("pause must be send-only")
	}
	// session:send: two web connections, receive on the second.
	s := byID["open-agents-session-send-web"]
	if s == nil || !hasStep(s, "ws_connect", "web-2") {
		t.Fatal("session:send needs a second web connection")
	}
}

func TestMissionSendCases_NoBridgeReal_EmitsNothing(t *testing.T) {
	if got := missionSendCases(missionSendFixture(), nil); got != nil {
		t.Fatalf("emitted %d cases without a real bridge", len(got))
	}
}
```

Helpers `caseByID`/`hasStep`/`workflowVocabFixture`: local to the test file; `hasStep(c, action, typeOrConn)` scans `c.Steps` matching `Action` plus (`Type` for ws_receive or `ConnectionID` for ws_connect).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestMissionSendCases -v`
Expected: FAIL (undefined: missionSendCases).

- [ ] **Step 3: Implement** `internal/head/scout/mission_send_cases.go`:

```go
package scout

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// missionSendCases emits the web-origin workflow sends the room relay routes
// at the REAL bridge (web→bridge via payload.deviceId) plus the web→web
// session:send pair. Reply expectations follow the bridge's actual behavior
// (spec §8): start/start_task echo task_started deterministically; pause and
// cancel only log — send-only steps whose coverage is send-side credit
// (coverage.go:286-293 credits a send whose connection maps the declared
// FromRole and whose ToRole is real). task_answer/task_guidance are NOT
// emitted here: their effect is conditional (pendingQuestion / live session)
// and they stay vocab partial marks.
func missionSendCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if !realRoles["bridge"] || svc.Protocol == nil || svc.Vocabulary == nil {
		return nil
	}
	deviceID := "{{bridge.deviceId}}"
	newCase := func(id, name string, steps []agent.TestStep) agent.TestCase {
		return agent.TestCase{ID: wsCaseID(svc.Name, "wf", id), Name: name,
			Service: svc.Name, Target: svc.URL, Action: "ws_flow",
			Expectation: name, Priority: 0.6, Steps: steps}
	}
	connect := agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"}
	var cases []agent.TestCase
	sendWithEcho := []struct{ send, echo, id, name string }{
		{"workflow:start_task", "workflow:task_started", "start-task",
			"web sends workflow:start_task, real bridge echoes workflow:task_started"},
		{"workflow:start", "workflow:task_started", "start",
			"web sends workflow:start with tasks[], real bridge echoes workflow:task_started per task"},
	}
	for _, e := range sendWithEcho {
		payload := map[string]any{"deviceId": deviceID}
		if e.send == "workflow:start" {
			// bridge.go:2601-2627 emits task_started only per payload.tasks item.
			payload["jobId"] = "{{case.missionId}}"
			payload["tasks"] = []any{map[string]any{"id": "t-seed"}}
		}
		cases = append(cases, newCase(e.id, e.name, []agent.TestStep{
			connect,
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny(e.send, payload)},
			{Action: "ws_receive", ConnectionID: "web", Type: e.echo, Timeout: 30},
		}))
	}
	for _, send := range []string{"workflow:pause", "workflow:cancel"} {
		cases = append(cases, newCase(sendTypeToID(send),
			fmt.Sprintf("web sends %s at the real bridge (send-side credit; bridge logs only)", send),
			[]agent.TestStep{
				connect,
				{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny(send, map[string]any{"deviceId": deviceID, "jobId": "{{case.missionId}}"})},
			}))
	}
	// web→web session:send — broadcast excludes the sender (room.ts:449-460),
	// so a second web connection receives it.
	cases = append(cases, newCase("session-send-web",
		"web session:send relayed to a second web connection",
		[]agent.TestStep{
			connect,
			{Action: "ws_connect", ConnectionID: "web-2", Role: "web"},
			{Action: "ws_send", ConnectionID: "web", Message: wsSendBodyAny("session:send", map[string]any{"deviceId": deviceID, "message": "hello from cerberus"})},
			{Action: "ws_receive", ConnectionID: "web-2", Type: "session:send", Timeout: 15},
		}))
	return cases
}

func sendTypeToID(typ string) string { return sanitizeTypeID(typ) }
```

Attach in `WSCasesCovered` (ws_cases.go, after the `realResponderCases` line):

```go
		// Web-origin workflow sends + session:send (see mission_send_cases.go).
		cases = append(cases, missionSendCases(svc, realRoles)...)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run TestMissionSendCases -v` → PASS; `go test ./internal/head/scout/` — full package green (self-play suppression does not drop these: they connect `web`, not a real role — ws_cases.go:65-71).

- [ ] **Step 5: Commit**

```bash
make check
git add internal/head/scout/mission_send_cases.go internal/head/scout/mission_send_cases_test.go internal/head/scout/ws_cases.go
git commit -m "feat(scout): missionSendCases — web-origin workflow sends + web-web session:send"
```

---

### Task 4: `missionSeedCases` — the orchestration chain

**Files:**
- Create: `internal/head/scout/mission_seed_cases.go`
- Modify: `internal/head/scout/ws_cases.go` (attach after `missionSendCases`)
- Test: `internal/head/scout/mission_seed_cases_test.go`

**Interfaces:**
- Consumes: Task 2 `Capture` + `{{case.*}}`; `AuthRole` admin injection (http_route_cases.go:33-41 precedent — requires an `admin` protocol role with `CredentialRef`, already present in dogfood); `wsSendBodyAny`, `sanitizeTypeID`, `wsCaseID` from sibling generators.
- Produces: `missionSeedCases(svc project.Service, realRoles map[string]bool) []agent.TestCase` — ONE case per service declaring workflow vocab edges.

- [ ] **Step 1: Write the failing test**

```go
package scout

import "testing"

func TestMissionSeedCases_SetupChainOrder(t *testing.T) {
	cases := missionSeedCases(missionSendFixture(), map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 mission case, got %d", len(cases))
	}
	steps := cases[0].Steps
	// 1 plan seed, 2 user plan update, 3 provider, 4 agent seed, 5 mission create.
	wantURLs := []string{
		"/api/admin/billing/plans", "/api/admin/users/", "/api/admin/ai-providers",
		"/api/agents", "/api/missions",
	}
	idx := 0
	for _, s := range steps {
		if s.Action != "http_request" {
			continue
		}
		if idx >= len(wantURLs) || !strings.Contains(s.URL, wantURLs[idx]) {
			t.Fatalf("http step %d URL %q, want prefix %q", idx, s.URL, wantURLs[idx])
		}
		idx++
	}
	if idx != len(wantURLs) {
		t.Fatalf("want %d http steps, got %d", len(wantURLs), idx)
	}
	// Plan payload must NOT set max_concurrent_tasks (spec §2: the 0-trap).
	planStep := firstStepWithURL(steps, "/api/admin/billing/plans")
	if strings.Contains(planStep.Body, "max_concurrent_tasks") {
		t.Fatal("plan payload must omit max_concurrent_tasks")
	}
	if !strings.Contains(planStep.Body, `"workflows":true`) || !strings.Contains(planStep.Body, "daily_missions") {
		t.Fatal("plan payload must gate workflows + raise daily_missions")
	}
	// Read-back wiring: plan step captures the id; user step substitutes it.
	if planStep.Capture["id"] != "planId" {
		t.Fatalf("plan step capture = %v", planStep.Capture)
	}
	userStep := firstStepWithURL(steps, "/api/admin/users/")
	if !strings.Contains(userStep.URL, "{{case.planId}}") && !strings.Contains(userStep.Body, "{{case.planId}}") {
		t.Fatal("user plan step must consume {{case.planId}}")
	}
}

func TestMissionSeedCases_ReceiveWindow(t *testing.T) {
	cases := missionSeedCases(missionSendFixture(), map[string]bool{"bridge": true})
	var recvTimeouts []int
	for _, s := range cases[0].Steps {
		if s.Action == "ws_receive" {
			recvTimeouts = append(recvTimeouts, s.Timeout)
		}
	}
	// Deterministic pushes get MINUTE-SCALE windows (default 10s fails them).
	for _, to := range recvTimeouts {
		if to < 120 {
			t.Fatalf("receive timeout %d < 120s", to)
		}
	}
	// The completion signal is job_status (out-of-band), never job_completed.
	for _, s := range cases[0].Steps {
		if s.Action == "ws_receive" && s.Type == "workflow:job_completed" {
			t.Fatal("job_completed is a dead type; completion is workflow:job_status")
		}
	}
}
```

(`strings` import, `firstStepWithURL` local helper.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestMissionSeedCases -v`
Expected: FAIL (undefined: missionSeedCases).

- [ ] **Step 3: Implement** `internal/head/scout/mission_seed_cases.go`:

```go
package scout

import (
	"os"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// missionSeedCases seeds ONE real mission and observes the orchestration
// pushes on a web connection. Setup unlocks the whole gating chain (spec
// §1-§6): plan feature gate → user plan → planner provider → agent row
// (stall guard) → mission create. Emitted only when the service declares
// workflow-family vocab edges and the bridge role is a real process.
func missionSeedCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if !realRoles["bridge"] || svc.Protocol == nil || svc.Vocabulary == nil || !hasWorkflowEdges(svc.Vocabulary) {
		return nil
	}
	admin := ""
	if r := svc.Protocol.Roles["admin"]; r != nil && r.CredentialRef != "" {
		admin = "admin"
	}
	plannerKey := os.Getenv("CERBERUS_PLANNER_API_KEY")
	plannerURL := os.Getenv("CERBERUS_PLANNER_API_URL")
	plannerModel := os.Getenv("CERBERUS_PLANNER_MODEL")
	steps := []agent.TestStep{
		// 1. Seed the plan. NEVER max_concurrent_tasks (spec §2 0-trap).
		{Action: "http_request", URL: svcBaseURL(svc) + "/api/admin/billing/plans", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: `{"name":"cerberus-dogfood","price_monthly":0,"limits":{"feature_gates":{"workflows":true},"rate_limits":{"daily_missions":9999}}}`,
			Capture: map[string]string{"id": "planId"}},
		// 2. Switch the user to it (id read back in step 1).
		{Action: "http_request", URL: svcBaseURL(svc) + "/api/admin/users/{{case.userId}}", Method: "PUT",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: `{"plan":"{{case.planId}}"}`},
	}
	if plannerKey != "" {
		steps = append(steps, agent.TestStep{
			// 3. Planner provider (encrypt-at-rest requires PROVIDER_KEY_KEK in .dev.vars — harness concern, Task 5).
			Action: "http_request", URL: svcBaseURL(svc) + "/api/admin/ai-providers", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: fmt.Sprintf(`{"name":"cerberus-planner","provider":"anthropic","api_url":%q,"api_key":%q,"models":[{"id":%q,"display_name":"planner","input_price_per_million":0,"output_price_per_million":0}],"is_active":true}`,
				plannerURL, plannerKey, plannerModel),
		})
	}
	steps = append(steps,
		// 4. Agent row — the stall guard (spec §5): user-scoped route.
		agent.TestStep{Action: "http_request", URL: svcBaseURL(svc) + "/api/agents", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: `{"name":"cerberus-bridge-agent","baseCli":"claude"}`,
			Capture: map[string]string{"id": "agentId"}},
		// 5. The mission itself.
		agent.TestStep{Action: "http_request", URL: svcBaseURL(svc) + "/api/missions", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: `{"inputText":"Reply with the single word done. Do not create files.","deviceIds":["{{bridge.deviceId}}"],"autoConfirm":true}`,
			Capture: map[string]string{"mission.id": "missionId"}},
		// 6. Observe on a web connection: deterministic pushes only.
		agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_started", Timeout: 600},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 600},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_result", Timeout: 600},
		// Completion signal is job_status (out-of-band type; job_completed is dead — spec §9).
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:job_status", Timeout: 600},
	)
	return []agent.TestCase{{
		ID: wsCaseID(svc.Name, "wf", "mission-seed"), Service: svc.Name, Target: svc.URL,
		Name:        "seeded mission drives the workflow family end to end",
		Action:      "ws_flow", Priority: 0.8,
		Expectation: "mission created (plan gate, provider, agent row seeded) and the real bridge executes it: task_started, task_progress, task_result observed on web; completion via workflow:job_status",
		Steps:       steps,
	}}
}

// hasWorkflowEdges: any vocab edge whose type carries the workflow: prefix.
func hasWorkflowEdges(v *project.Vocabulary) bool {
	for _, e := range v.Edges {
		if strings.HasPrefix(e.Type, "workflow:") && !e.Partial && !e.Unsupported {
			return true
		}
	}
	return false
}
```

Notes for the implementer:
- `svcBaseURL(svc)` = `serviceHost(svc.URL)`-style http(s) form; follow `httpRouteCases` (line 55) — reuse its helper, converting ws→http is NOT needed there (verify which form `serviceHost` returns and mirror `httpRouteCases` exactly).
- `{{case.userId}}` / `{{bridge.deviceId}}` must resolve: add `userId` to the admin actor's params in the dogfood `project.yaml` if absent (mirror how `deviceId` is declared for bridge — same file, real precedent). If the admin actor's user id is not statically known, capture it instead: prepend an `http_request GET /api/auth/me`-style step with `Capture: {"id": "userId"}` — choose whichever matches the actual auth routes (check `apps/api/src/routes/auth.ts`).
- The exact agent `baseCli` value (`"claude"`) must equal one of the bridge PTY's `cliEnabled` capabilities — read `dogfood/realtime-e2e/.cerberus/project.yaml` actors and use that literal.
- Attach in `WSCasesCovered`: `cases = append(cases, missionSeedCases(svc, realRoles)...)`.

- [ ] **Step 4: Run tests + full package**

Run: `go test ./internal/head/scout/ -run TestMissionSeedCases -v` → PASS; full scout package green.

- [ ] **Step 5: Commit**

```bash
make check
git add internal/head/scout/mission_seed_cases.go internal/head/scout/mission_seed_cases_test.go internal/head/scout/ws_cases.go dogfood/realtime-e2e/.cerberus/project.yaml
git commit -m "feat(scout): missionSeedCases — gate-chain setup + orchestration observation"
```

---

### Task 5: Dogfood harness env + open-agents `.dev.vars`

**Files:**
- Modify: `../open-agents/.dev.vars` (add `PROVIDER_KEY_KEK`)
- Modify: dogfood run docs / harness script where wrangler env is set (locate via `make integration-openagents` target and the realtime-e2e harness scripts)

**Interfaces:**
- Consumes: Task 4's env vars (`CERBERUS_PLANNER_API_KEY/URL/MODEL`).
- Produces: a live environment where `POST /api/admin/ai-providers` returns 2xx.

- [ ] **Step 1: Generate a KEK and add it to `.dev.vars`** (open-agents repo, NOT committed if gitignored — check `git -C ../open-agents status` after edit):

```bash
openssl rand -hex 32
```

Append to `../open-agents/.dev.vars`: `PROVIDER_KEY_KEK=<generated>`. Set before wrangler starts (harness starts wrangler per run — spec §6/round-2 #6).

- [ ] **Step 2: Wire the planner env** in the realtime-e2e harness (where `ANTHROPIC_AUTH_TOKEN` is already exported for the heads — memory: live runs need it). Add:

```bash
export CERBERUS_PLANNER_API_KEY="$ANTHROPIC_AUTH_TOKEN"
export CERBERUS_PLANNER_API_URL="https://api.anthropic.com/v1/"   # confirm against the endpoint the heads actually use
export CERBERUS_PLANNER_MODEL="claude-sonnet-5"                    # confirm: must be servable by that endpoint
```

- [ ] **Step 3: Smoke-verify the gate chain by hand (throwaway, not a test file)**: start wrangler, run the five setup requests with curl using an admin JWT from `POST /api/auth/dev/setup` (superadmin) — plan 2xx, user PUT 2xx, provider 2xx, `POST /api/agents` 2xx, `POST /api/missions` 2xx with `status:"running"` in the response. If any step fails, fix env/payload here, NOT in the generator (generator already unit-tested).

- [ ] **Step 4: Commit** (cerberus-side harness/docs changes only; `.dev.vars` stays out of git):

```bash
make check
git add <harness files touched>
git commit -m "chore(dogfood): planner provider env wiring for workflow coverage"
```

---

### Task 6: Live validation run + docs + memory

**Files:**
- Modify: `cerberus-docs/technical/dogfood/2026-08-17-http-vocab-first-run.md` (third update section) or a new `2026-08-18-workflow-coverage-run.md`
- Memory: new vault file + `MEMORY.md` line

- [ ] **Step 1: Run realtime-e2e** (same setup as run9: wrangler :8989 + bridge PTYs + real heads, actors=4). Success criteria (spec):
  - mission completes: `workflow:job_status` completion received; 0 fail verdicts
  - 15 workflow edges covered + 2 marked + `session:send` covered
  - coverage ≈ 98.5% (arithmetic: N 393 → 391 with marks; 385/391)
  - claims gate unchanged (1 proven / 0 emulated-only; no exit 3)
- [ ] **Step 2: If edges remain uncovered**, diagnose from the gap list (vocab marks vs missing frames) — update vocab notes for anything conditionally absent (`task_question` etc. → partial with reason), never force a pass.
- [ ] **Step 3: Write the run doc** (coverage progression 93.9% → final, drift observation on the mission case, any new open-agents findings).
- [ ] **Step 4: Commit + memory**

```bash
git add cerberus-docs/
git commit -m "docs: workflow orchestration coverage live run"
```

Memory file `workflow-orchestration-coverage-shipped.md` (type: project) with merge SHA, final coverage, and the two open items (planner model choice, 6 residual gaps).

---

## Self-Review (done)

- Spec coverage: gating chain (Task 4 steps 1-5 + Task 5 env), web actor sends + echo semantics (Task 3), session:send (Task 3), dead-type marks (Task 1), capture/read-back (Task 2), long windows (Task 4), claims non-binding (no `Claims` field set on any new case), error handling (hard-fail setup + stall = case failure via unreceived completion), testing (unit per task + Task 6 live). 6 non-workflow gaps: explicitly out of scope. ✓
- Placeholders: Task 4 implementer notes flag three verify-at-implementation points (admin userId capture, baseCli literal, planner endpoint/model) — each has a concrete resolution path, no TBDs. ✓
- Type consistency: `missionSendCases`/`missionSeedCases(svc project.Service, realRoles map[string]bool) []agent.TestCase` consistent across Tasks 3/4 and the `WSCasesCovered` attach; `Capture map[string]string`, `captureFromHTTPBody`, `substituteCaseParams` consistent between Task 2 definition and Task 4 use. ✓

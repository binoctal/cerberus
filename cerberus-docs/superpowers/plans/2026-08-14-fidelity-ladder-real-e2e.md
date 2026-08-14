# Fidelity Ladder & Real E2E Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give cerberus per-actor fidelity declaration (`emulated | real-process`), a generic external-process harness, and a second dogfood project that verifies open-agents' core promise with real bridge processes and a real CLI.

**Architecture:** Three additions: (1) `Actor.fidelity` + `Actor.process` schema with validation and a run-summary watermark; (2) a session-scoped process harness in `internal/session` that pairs/starts/captures/kills external processes declared in project.yaml; (3) `dogfood/realtime-e2e/` with two real bridges plus integration tests for the L1/M1/L2 ladder. Cross-actor templating `{{actor.param}}` lets the emulated web actor route to real bridges.

**Tech Stack:** Go 1.25 (module `github.com/binoctal/cerberus`), modernc.org/sqlite, open-agents bridge (Go, sibling repo at `../open-agents`), Cloudflare Workers dev server (wrangler, port 8989).

**Spec:** `cerberus-docs/superpowers/specs/2026-08-14-fidelity-manifest-real-e2e-design.md`

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By lines.
- Comments and commit messages in English.
- No CGo. Docs only under `cerberus-docs/`.
- Work happens on branch `feat/fidelity-manifest-real-e2e` (already created), NOT a worktree (cross-repo dogfood convention).
- All tests: `go test -race ./...`; integration tests use build tag `integration` and are run explicitly.
- open-agents local dev facts (verified 2026-08-14, see spec): api on :8989 (`npm run dev` in `../open-agents/apps/api`), `POST /api/dev/setup` reuses the dev user and creates a new device per call, bridge config at `$HOME/.open-agents-bridge/config.json`, `pair --dev --server http://localhost:8989 -d <name>` + `start -d <name>`, missions need `users.plan` with workflows gate, orchestrator alarm `POST /api/missions/internal/orchestrator/alarm` `{type:"scheduleNext",payload:{missionId}}` needs `INTERNAL_SECRET` in `apps/api/.dev.vars` (currently missing) and `API_BASE_URL` currently hijacks DO callbacks to a capture server (must be emptied for real E2E, restored after).

---

### Task 1: Fidelity manifest schema + validation

**Files:**
- Modify: `internal/project/schema.go` (Actor struct)
- Modify: `internal/project/validate_actors.go`
- Test: `internal/project/schema_test.go`

**Interfaces:**
- Produces: `Actor.Fidelity string` (`""`, `"emulated"`, `"real-process"`), constants `FidelityEmulated = "emulated"`, `FidelityRealProcess = "real-process"`, `Actor.Process *ProcessSpec` with:

```go
// ProcessSpec declares an external process actor (fidelity: real-process).
// All SUT-specific facts live here in YAML; cerberus stays generic.
type ProcessSpec struct {
	// Workdir is the child process working directory (optional).
	Workdir string `yaml:"workdir,omitempty"`
	// Setup is a one-shot provisioning command run to completion before Start
	// (e.g. bridge pairing). Empty argv means no setup step.
	Setup []string `yaml:"setup,omitempty"`
	// Start is the long-running process argv (required).
	Start []string `yaml:"start"`
	// Env overrides the child environment. Values support the same templates
	// as Start entries ({{runtime.dir}} / {{actor.name}}).
	Env map[string]string `yaml:"env,omitempty"`
	// CaptureFile is a JSON file read after Setup; CaptureJSON maps
	// param name -> dot-path into that JSON (e.g. deviceId: devices.b1.deviceId).
	// Captured values merge into the actor's runtime PathParams.
	CaptureFile  string            `yaml:"capture_file,omitempty"`
	CaptureJSON  map[string]string `yaml:"capture_json,omitempty"`
	// ReadyPattern is a regex on combined child stdout/stderr; the harness
	// waits for it before declaring the actor ready. Empty = no wait.
	ReadyPattern string `yaml:"ready_pattern,omitempty"`
	// ReadyTimeout bounds the readiness wait (Go duration string). Default 30s.
	ReadyTimeout string `yaml:"ready_timeout,omitempty"`
}
```

- [ ] **Step 1: Write the failing tests** in `internal/project/schema_test.go`:

```go
func TestValidateActors_Fidelity(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string // substring of validation error, "" = valid
	}{
		{"default emulated ok", `
actors:
  - name: web
    credentials: {}
`, ""},
		{"real-process with process block ok", `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
    process:
      start: ["sleep", "60"]
`, ""},
		{"real-process without process block rejected", `
actors:
  - name: b1
    credentials: {}
    fidelity: real-process
`, "process block is required"},
		{"emulated with process block rejected", `
actors:
  - name: b1
    credentials: {}
    fidelity: emulated
    process:
      start: ["sleep", "60"]
`, "must not have a process block"},
		{"unknown fidelity rejected", `
actors:
  - name: b1
    credentials: {}
    fidelity: simulated
`, `unknown fidelity "simulated"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := loadYAML[Config](tc.yaml) // use the test helper already in schema_test.go
			if err != nil {
				t.Fatal(err)
			}
			ve := &ValidationError{}
			validateActors(cfg, ve)
			if tc.want == "" && len(ve.Errors) > 0 {
				t.Fatalf("expected valid, got %v", ve.Errors)
			}
			if tc.want != "" && !strings.Contains(strings.Join(ve.Errors, "; "), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, ve.Errors)
			}
		})
	}
}
```

(Adapt `loadYAML` to the existing helper name in `schema_test.go`; if none parses raw YAML, unmarshal with `gopkg.in/yaml.v3` directly.)

- [ ] **Step 2: Run** `go test ./internal/project/ -run TestValidateActors_Fidelity -v` — expect FAIL (no fidelity handling).
- [ ] **Step 3: Implement** — add `Fidelity`/`Process` fields to `Actor` in `schema.go`, `ProcessSpec` type, constants, and in `validate_actors.go` add:

```go
func validateFidelity(actorIdx int, a Actor) string {
	switch a.Fidelity {
	case "", FidelityEmulated:
		if a.Process != nil {
			return fmt.Sprintf("actors[%d]: fidelity %q must not have a process block", actorIdx, FidelityEmulated)
		}
	case FidelityRealProcess:
		if a.Process == nil || len(a.Process.Start) == 0 {
			return fmt.Sprintf("actors[%d]: fidelity %q requires a process block with start", actorIdx, FidelityRealProcess)
		}
	default:
		return fmt.Sprintf("actors[%d]: unknown fidelity %q (supported: emulated, real-process)", actorIdx, a.Fidelity)
	}
	return ""
}
```

Call it from `validateActors` next to `validateAuthFlow`.

- [ ] **Step 4: Run** `go test ./internal/project/ -race` — PASS.
- [ ] **Step 5: Commit:** `git add internal/project/ && git commit -m "feat(project): per-actor fidelity manifest (emulated | real-process)"`

---

### Task 2: Fidelity watermark in run summary

**Files:**
- Modify: `internal/session/summary.go` (SessionSummary + String)
- Modify: `internal/session/run_phases_lifecycle.go:70` (buildSummary)
- Test: `internal/session/summary_test.go`

**Interfaces:**
- Consumes: Task 1 constants.
- Produces: `SessionSummary.RealActors []string` (JSON `real_actors,omitempty`) and `SessionSummary.AllEmulated bool` (JSON `all_emulated`). String() appends `\n  Fidelity: emulated-only` when AllEmulated, else `\n  Real actors: b1, b2` when RealActors is non-empty.

- [ ] **Step 1: Failing test** — construct `SessionSummary{AllEmulated: true}`, assert `String()` contains `emulated-only`; one with `RealActors: []string{"b1"}` asserts `Real actors: b1` and no `emulated-only`. Add a `buildSummary`-level test asserting fields populate from `rp.session.Cfg.Actors` (mirror how existing summary tests construct a runPhase; see `internal/session/lifecycle_test.go` for the pattern).
- [ ] **Step 2: Run** `go test ./internal/session/ -run Summary -v` — FAIL.
- [ ] **Step 3: Implement** — add fields; in `String()` after the coverage line:

```go
	if s.AllEmulated {
		out += "\n  Fidelity: emulated-only (all actors self-played)"
	} else if len(s.RealActors) > 0 {
		out += "\n  Real actors: " + strings.Join(s.RealActors, ", ")
	}
```

In `buildSummary`, before `FromResults` returns: iterate `rp.session.Cfg.Actors`; collect names where `Fidelity == project.FidelityRealProcess`; `AllEmulated = len(cfg.Actors) > 0 && len(real) == 0`. (If `Session` doesn't hold the project `Config`, find where the actors are reachable — `resolveActorAuth` at `internal/session/lifecycle_run.go:33` iterates actors; reuse the same accessor and, if needed, stash the config on `Session` during initialize.)

- [ ] **Step 4: Run** `go test ./internal/session/ -race` — PASS.
- [ ] **Step 5: Commit:** `git commit -m "feat(session): fidelity composition watermark in run summary"`

---

### Task 3: Process harness (setup / capture / start / ready / teardown)

**Files:**
- Create: `internal/session/harness.go`
- Create: `internal/session/harness_test.go`
- Modify: `internal/session/auth_setup.go` (launch hook), `internal/session/lifecycle_run.go` (teardown hook)
- Modify: `internal/head/agent/websocket.go` (cross-actor `{{actor.param}}` templating)

**Interfaces:**
- Consumes: `project.ProcessSpec`, `project.Actor` (Task 1).
- Produces:

```go
// harness manages real-process actors for one session.
type harness struct {
	log      *zap.Logger
	runtime  string // session runtime dir for {{runtime.dir}}
	mu       sync.Mutex
	procs    map[string]*harnessProc
}

type harnessProc struct {
	name    string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
}

// LaunchActor provisions and starts one real-process actor. It runs Setup to
// completion, applies Capture (merging into actor.Credentials.PathParams so
// {{actor.param}} templating can see the values), starts the child in its own
// process group, and waits for ReadyPattern. Any error fails the launch.
func (h *harness) LaunchActor(ctx context.Context, actor *project.Actor) error

// StopAll terminates every child process group (SIGTERM, 5s grace, SIGKILL).
func (h *harness) StopAll()
```

- [ ] **Step 1: Failing tests** in `harness_test.go` — use only POSIX tools as fake children:

```go
func TestHarness_SetupCaptureStartReady(t *testing.T) {
	dir := t.TempDir()
	// setup writes a JSON file the harness then captures from
	setupScript := "#!/bin/sh\n" + fmt.Sprintf(`echo '{"devices":{"b1":{"deviceId":"device_x","deviceToken":"tok"}}}' > %s/cfg.json`, dir)
	writeScript(t, dir+"/setup.sh", setupScript)
	spec := &project.ProcessSpec{
		Setup:  []string{dir + "/setup.sh"},
		Start:  []string{"sh", "-c", "echo BRIDGE_READY; exec sleep 60"},
		CaptureFile:  dir + "/cfg.json",
		CaptureJSON:  map[string]string{"deviceId": "devices.b1.deviceId"},
		ReadyPattern: `BRIDGE_READY`,
	}
	h := newHarness(zap.NewNop(), dir)
	actor := &project.Actor{Name: "b1", Process: spec}
	if err := h.LaunchActor(t.Context(), actor); err != nil {
		t.Fatal(err)
	}
	defer h.StopAll()
	if got := actor.Credentials.PathParams["deviceId"]; got != "device_x" {
		t.Fatalf("captured deviceId = %q, want device_x", got)
	}
}

func TestHarness_ReadyPatternTimeout(t *testing.T) { // child never prints pattern -> error, child killed
	spec := &project.ProcessSpec{
		Start:        []string{"sleep", "60"},
		ReadyPattern: `NEVER_APPEARS`,
		ReadyTimeout: "500ms",
	}
	h := newHarness(zap.NewNop(), t.TempDir())
	if err := h.LaunchActor(t.Context(), &project.Actor{Name: "b1", Process: spec}); err == nil {
		h.StopAll()
		t.Fatal("expected readiness timeout error")
	}
	h.StopAll()
}

func TestHarness_StopAllKillsProcessGroup(t *testing.T) { // child spawns a sub-sleep; after StopAll both must be gone (check via pgrep -f marker)
}
```

- [ ] **Step 2: Run** `go test ./internal/session/ -run TestHarness -v` — FAIL (no harness).
- [ ] **Step 3: Implement `harness.go`**:

```go
package session

import (
	"context", "encoding/json", "fmt", "os", "os/exec", "regexp", "strings", "syscall", "time"
	"go.uber.org/zap"
	"github.com/binoctal/cerberus/internal/project"
)

func newHarness(log *zap.Logger, runtimeDir string) *harness {
	return &harness{log: log, runtime: runtimeDir, procs: map[string]*harnessProc{}}
}

// tmpl resolves {{runtime.dir}} and {{actor.name}} in argv/env entries.
func (h *harness) tmpl(s string, actor *project.Actor) string {
	r := strings.ReplaceAll(s, "{{runtime.dir}}", h.runtime)
	return strings.ReplaceAll(r, "{{actor.name}}", actor.Name)
}

func (h *harness) LaunchActor(ctx context.Context, actor *project.Actor) error {
	spec := actor.Process
	if len(spec.Setup) > 0 {
		cmd := exec.CommandContext(ctx, h.tmpl(spec.Setup[0], actor), h.tmplSlice(spec.Setup[1:], actor)...)
		cmd.Env = h.childEnv(spec, actor)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("harness %s: setup failed: %w: %s", actor.Name, err, out)
		}
	}
	if spec.CaptureFile != "" && len(spec.CaptureJSON) > 0 {
		raw, err := os.ReadFile(h.tmpl(spec.CaptureFile, actor))
		if err != nil {
			return fmt.Errorf("harness %s: capture file: %w", actor.Name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("harness %s: capture file not JSON: %w", actor.Name, err)
		}
		if actor.Credentials.PathParams == nil {
			actor.Credentials.PathParams = map[string]string{}
		}
		for param, path := range spec.CaptureJSON {
			v, err := dotPath(doc, path) // same dot-path semantics as authflow; reuse agent's extractByDotPath via a small local copy to avoid an import cycle
			if err != nil {
				return fmt.Errorf("harness %s: capture %s: %w", actor.Name, path, err)
			}
			actor.Credentials.PathParams[param] = v
		}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, h.tmpl(spec.Start[0], actor), h.tmplSlice(spec.Start[1:], actor)...)
	cmd.Env = h.childEnv(spec, actor)
	cmd.Dir = h.tmpl(spec.Workdir, actor)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // own process group for group kill
	cmd.Stdout = io.MultiWriter(scanSink{pattern: readyRe, hit: make(chan struct{}, 1)}, os.Stderr) // see note below
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("harness %s: start: %w", actor.Name, err)
	}
	h.mu.Lock(); h.procs[actor.Name] = &harnessProc{name: actor.Name, cmd: cmd, cancel: cancel}; h.mu.Unlock()
	if readyRe != nil {
		timeout := 30 * time.Second
		if spec.ReadyTimeout != "" { timeout = mustParseDuration(spec.ReadyTimeout) }
		select {
		case <-hit:
		case <-time.After(timeout):
			h.stopOne(actor.Name)
			return fmt.Errorf("harness %s: ready pattern %q not seen within %s", actor.Name, spec.ReadyPattern, timeout)
		}
	}
	return nil
}
```

Notes for the implementer: (a) wire the scan sink so it closes `hit` once; compile the regex from `spec.ReadyPattern` (empty ⇒ nil ⇒ no wait); (b) `StopAll`/`stopOne`: `syscall.Kill(-pgid, SIGTERM)`, wait 5s, then SIGKILL, then `cancel()` and `cmd.Wait()`; (c) `childEnv` = `os.Environ()` + templated `spec.Env` overrides.

- [ ] **Step 4: Hook into the session lifecycle**:
  - `internal/session/lifecycle_run.go:33` — after `s.resolveActorAuth(ctx)` add `s.launchRealProcessActors(ctx)`; in `rp.finalize()` add `s.harnessStopAll()`. Both methods live in `harness.go`; failures log-and-continue for launch? NO — a real-process actor that fails to launch makes every case touching it fail misleadingly: fail the run early (`rp.initialize` style error) instead. Find `Session` struct (in `lifecycle_types.go` or `session.go`) to add `harness *harness`.
  - `launchRealProcessActors` iterates `cfg.Actors` where `Fidelity == project.FidelityRealProcess`, calls `LaunchActor`, sets `s.harness`.
  - IMPORTANT: real-process actors must NOT get an emulated WS connection. In the WS case generators (`internal/head/scout/ws_cases.go`), a role whose `credential_ref` names a real-process actor is excluded from `ws_connect` case generation — add a guard in `wsCasesForService` that skips generating connect/send steps for roles bound to real-process actors (the generator receives the project config via `svc`; thread the actor fidelity through the same path `wsRelayCoverageCases` gets its data). Cases that need the real bridge are generated by the E2E generator (Task 7), which references the actor's captured params instead of connecting as it.

- [ ] **Step 5: Cross-actor templating** — in `internal/head/agent/websocket.go`, send-body templating resolves `{{param}}` and `{{role.param}}` for the sending connection's credentialRef (`websocket.go:59`). Extend the resolver: a token matching `{{<actorName>.<param>}}` where `<actorName>` names another configured actor resolves from THAT actor's runtime `Credentials.PathParams`. The executor already stores per-actor params (see `pathParamsFor(credentialRef)`); expose the lookup by actor name and try it as a fallback before failing the template. Failing test first in `websocket_test.go`: two actors, `b1` has PathParams `{"deviceId":"device_x"}`, send as `web` with body `{"type":"session:start","deviceId":"{{b1.deviceId}}"}`, assert the outgoing frame contains `device_x`.

- [ ] **Step 6: Run** `go test ./internal/session/ ./internal/head/agent/ -race` — PASS. Then `make lint`.
- [ ] **Step 7: Commit:** `git commit -m "feat(session): external-process actor harness + cross-actor templating"`

---

### Task 4: dogfood/realtime-e2e project (config only)

**Files:**
- Create: `dogfood/realtime-e2e/.cerberus/project.yaml`
- Create: `dogfood/realtime-e2e/.cerberus/protocols/open-agents.yaml` (copy of `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — same SUT)
- Create: `dogfood/realtime-e2e/.gitignore` (`runtime/`)

**Interfaces:**
- Consumes: Tasks 1-3 schema.
- Produces: a loadable dogfood project (loader round-trip is the test).

- [ ] **Step 1: Write `project.yaml`** (real bridge paths relative to the dogfood dir):

```yaml
project:
  name: realtime-e2e-dogfood
services:
  - name: realtime
    url: http://localhost:8989/ws/{userId}
    protocol_ref: open-agents
actors:
  - name: web-actor
    credentials:
      token: demo_token
    auth:
      login: {method: POST, path: /api/dev/setup, body: {}, headers: {Origin: "http://localhost:8989"}}
      inject_as: "Authorization: Bearer {token}"
      path_params: {userId: config.userId}
      http_login: {method: POST, path: /api/dev/login, body: {}, headers: {Origin: "http://localhost:8989"}}
      http_token_from: token
  - name: bridge-pty-1
    credentials: {}
    fidelity: real-process
    process:
      workdir: "../../../open-agents/bridge"
      setup: ["./build/open-agents-bridge", "pair", "--dev", "--server", "http://localhost:8989", "-d", "bridge-pty-1"]
      start: ["./build/open-agents-bridge", "start", "-d", "bridge-pty-1"]
      env:
        HOME: "{{runtime.dir}}/bridge-pty-1-home"   # isolates .open-agents-bridge/config.json per instance
      capture_file: "{{runtime.dir}}/bridge-pty-1-home/.open-agents-bridge/config.json"
      capture_json: {deviceId: "devices.bridge-pty-1.deviceId"}
      ready_pattern: "connected|WebSocket"
      ready_timeout: 30s
  - name: bridge-pty-2   # identical block with -2 suffixes (second device, same user/room)
    credentials: {}
    fidelity: real-process
    process: {…same shape, all names bridge-pty-2…}
settings:
  max_duration: 12m
  confidence_threshold: 0.7
  ai_budget:
    session_total_tokens: 200000
    per_call_limit: 10000
```

- [ ] **Step 2: Verify facts before relying on them** (explicit probe steps, adjust YAML if reality differs):
  - `cd ../open-agents/bridge && make build && ./build/open-agents-bridge pair --help` — confirm `-d/--device` flag and that `pair --dev` accepts `--server`.
  - Run the pair command once by hand with `HOME=$PWD/tmphome`, inspect `tmphome/.open-agents-bridge/config.json` — confirm the `devices.<name>.deviceId` dot-path and that `start -d bridge-pty-1` prints a connect-confirmation line (set `ready_pattern` to that exact text).
  - `grep -n "getCLICommand" ../open-agents/bridge/internal/session/manager.go` and read it — record which cliType maps to `bash`-style PTY with a custom `command` (for L1) and which to `claude` (for L2). Write the finding into this file's Task 5 notes before implementing Task 5.
- [ ] **Step 3: Loader test** — `dogfood/realtime-e2e/config_test.go` mirroring `dogfood/ws-realtime/main_test.go:138-176` (`TestProjectConfig_Loads`): assert two real-process actors load, templates intact, validation passes.
- [ ] **Step 4: Commit:** `git commit -m "feat(dogfood): realtime-e2e project with two real-process bridge actors"`

---

### Task 5: L1 — real bridge + PTY full chain (integration)

**Files:**
- Create: `internal/head/agent/realbridge_l1_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: harness (Task 3), dogfood config (Task 4), existing integration helpers (`openagents_setup_test.go:35` `setupOpenAgents`, `relaycoverage_probe_integration_test.go:26-38` vocab-loading pattern, `newStepExecutionWithIdx`).

- [ ] **Pre-flight (documented, manual/scripted steps in the test's TestMain or a helper, following `make integration-openagents` conventions):**
  1. Start api: `setsid` + fnm node 22 + `npm run dev` in `../open-agents/apps/api` (port 8989), wait for `/health`.
  2. Edit `../open-agents/apps/api/.dev.vars`: add `INTERNAL_SECRET=cerberus-dogfood-secret`, comment out `API_BASE_URL` (hijacks DO→Worker callbacks otherwise). Keep a backup; the suite restores it in teardown.
  3. `cd ../open-agents/bridge && make build`.
- [ ] **Step 1: Write the test** — one test function, table of subtests, each driving the web side over WS while a REAL bridge process runs:

```go
//go:build integration

func TestRealBridge_L1_PTYSessions(t *testing.T) {
	srv := setupOpenAgents(t)          // starts wrangler api + dev user, returns base URL + creds
	h := launchRealBridges(t, srv)     // harness from Task 3 via dogfood/realtime-e2e config; t.Cleanup(h.StopAll)
	b1 := h.Actor("bridge-pty-1")      // captured deviceId available after ready

	t.Run("session lifecycle", func(t *testing.T) {
		// 1. web connects: ws_connect role=web, await devices:sync
		// 2. ws_send session:start {deviceId: "{{bridge-pty-1.deviceId}}", sessionId: "e2e-l1", cliType: <pty-cliType from Task 4 probe>, workDir: "/tmp", cols: 80, rows: 24, command: "bash"}
		// 3. ws_receive session:started (from REAL bridge)
		// 4. ws_send session:send {sessionId: "e2e-l1", input: "echo CERBERUS_L1_OK\n"}
		// 5. ws_receive session:output-batch whose payload contains "CERBERUS_L1_OK" (real PTY echo through BOTH batching layers)
		// 6. ws_send session:stop; ws_receive session:stopped
		// 7. persistence: HTTP GET /api/sessions/e2e-l1/messages (web JWT) — assert a message exists written by the bridge (POST /api/bridge/messages path)
	})
	t.Run("device:online seen on web", func(t *testing.T) { /* connect web, assert device:online for b1.deviceId arrived (or was in devices:sync) */ })
}
```

Steps 1-6 use `newStepExecutionWithIdx` exactly like `relaycoverage_probe_integration_test.go:46-68` (`probeEdge`) — no Examiner/escalation involvement.

- [ ] **Step 2: Run** `go test -tags integration ./internal/head/agent/ -run TestRealBridge_L1 -v -timeout 10m` — expect initial failures; debug against bridge stdout (harness logs child output) and api logs. Known likely friction, in order: CSRF Origin on any HTTP helper (add Origin header), WS web auth needs JWT not demo_token for the query token in this deployment (use http_login JWT), `command` field only honored for certain cliTypes (Task 4 probe resolves).
- [ ] **Step 3: When green, commit:** `git commit -m "test(integration): L1 real-bridge PTY session lifecycle e2e"`

---

### Task 6: M1 — deterministic multi-device orchestration (integration)

**Files:**
- Create: `internal/head/agent/realbridge_m1_integration_test.go` (`//go:build integration`)

**Interfaces:**
- Consumes: Task 5 helpers (`setupOpenAgents`, `launchRealBridges`).

- [ ] **Step 1: One-time D1 prep inside the test** (wrangler d1 execute against the dev DB, or the admin API with a superadmin JWT):
  - `UPDATE users SET plan='pro' WHERE email='dev@openagents.local'` (workflows gate, `routes/missions.ts:58-68`).
- [ ] **Step 2: Seed a deterministic mission** (no planner LLM): insert one `multiagent_missions` row (`status='running'`, `user_id=<dev user>`) and two `multiagent_tasks` rows (`status='pending'`, distinct `device_id` targeting each real bridge's captured deviceId, `cli_type` from Task 4 probe, `worktree_branch` distinct). Verify exact table/column names first: `ls ../open-agents/migrations` + grep `multiagent_missions`/`multiagent_tasks` CREATE TABLE — adjust the INSERTs to reality; record the confirmed schema as a comment in the test file.
- [ ] **Step 3: Trigger scheduling** — `POST /api/missions/internal/orchestrator/alarm` header `X-Internal-Secret: cerberus-dogfood-secret`? NO — read `routes/missions.ts:229` middleware: it compares `c.env.INTERNAL_SECRET` against the header it actually checks (verify header name, likely `X-Internal-Secret`); body `{"type":"scheduleNext","payload":{"missionId":"<id>"}}`.
- [ ] **Step 4: Assert routing + parallelism on the web WS**:
  - two `workflow:task_assign` messages received, each with `payload.deviceId` equal to the intended real bridge's deviceId (capability/targeting routing);
  - both assigns arrive before any completion event (parallel dispatch);
  - each real bridge attempts its task — for an unavailable cliType this deterministically yields `workflow:task_started` then `workflow:task_error`/`task_failed` from the REAL bridge; assert at least one task lifecycle event originated from each device (proves the assign reached a real process and its result callback chain DO→orchestrator→status update works: poll `GET /api/missions/<id>` until the task status changes from pending).
- [ ] **Step 5: Run** `go test -tags integration ./internal/head/agent/ -run TestRealBridge_M1 -v -timeout 10m`; debug; commit: `git commit -m "test(integration): M1 deterministic multi-device orchestration with real bridges"`

---

### Task 7: L2 — real CLI scheduled (integration, cost-bounded) + dogfood promotion

**Files:**
- Create: `internal/head/agent/realbridge_l2_integration_test.go` (`//go:build integration`)
- Modify: `internal/head/scout/ws_cases.go` (realE2E case family)

**Interfaces:**
- Consumes: Tasks 3-5.

- [ ] **Step 1: L2 test** — same skeleton as L1 but `cliType: claude` (ACP or PTY per Task 4 probe of `getCLICommand`), no `command` override; child env gains GLM credentials by adding to the dogfood actor's `process.env`: `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL` (values via environment passthrough — write them into the YAML from env at test setup, or reference `{{env.ANTHROPIC_AUTH_TOKEN}}` if the harness supports it — if not, add a small `{{env.NAME}}` template case to `harness.tmpl` with a unit test). Then:
  1. `session:start` with a fresh sessionId → `session:started`
  2. `session:send` input: `Reply with exactly the word READY and nothing else.` 
  3. `ws_receive` (60s window, poll receive) any of `session:output-batch`/`chat:response` whose combined content contains `READY`
  4. `session:stop` → `session:stopped`
  
  **Hard cost bound:** single prompt, assert then stop immediately; skip (t.Skip) when `ANTHROPIC_AUTH_TOKEN` is unset so CI-free environments don't fail.
- [ ] **Step 2: Run it**; debug (likely friction: claude binary PATH inside the bridge child — bridge runs with our env so PATH inherits; npx availability for ACP mode; first-run onboarding of the CLI in the isolated HOME — pre-seed `~/.claude` settings into the isolated HOME dir via the setup command if needed).
- [ ] **Step 3: Promote the ladder into the autonomous run** — add `realE2ECases(svc, cfg)` to `internal/head/scout/ws_cases.go`: when the project has ≥1 real-process actor bound to a protocol role, emit deterministic 6-step `ws_flow` cases (the L1/M1 scenarios as plan cases, referencing `{{bridge-pty-1.deviceId}}`), deduped against existing case keys like `wsRelayCoverageCases` does. Unit-test the generator with a fixture config (two real actors) asserting exactly the L1 case shape; the M1 seeding stays an integration test (D1 writes are out of scope for autonomous runs this session).
- [ ] **Step 4: Full autonomous run** in `dogfood/realtime-e2e/`:
  ```
  cd dogfood/realtime-e2e
  CERBERUS_MIGRATION_DIR=../../migrations ANTHROPIC_AUTH_TOKEN=… ANTHROPIC_BASE_URL=… ../../build/cerberus run
  ```
  Health gate (all four must be 0 in the log): `grep -c 'judge failed'`, `grep -c 'insufficient budget'`, `grep -c '"target":""'`, hallucinated-id pattern. Assert summary shows `Real actors: bridge-pty-1, bridge-pty-2` (Task 2 watermark) and L1 cases pass.
- [ ] **Step 5: Commit:** `git commit -m "test(integration): L2 real claude CLI + autonomous-run realE2E cases"`

---

### Task 8: Deferred track (explicit hand-off, no implementation this session)

Recorded so they are not lost (they belong to the emulated/breadth track):
1. **Negative/exception case family** — rate limits (bridge 200msg/s, web plan, >1MB close 1009, over-limit 1008), `MISSING_DEVICE_ID`, error frames, JWT-without-exp, CSRF, SEC-1 IDOR. Deterministic, emulated layer.
2. **HTTP route vocab extraction** — Hono route extractor for `vocabextract` + HTTP-edge coverage attribution.
3. **Examiner `deriveDimensions`** — ordering (output-batch order) and count (no-loss/no-dup) derivation.
4. **open-agents protocol inconsistencies** found during research (DO whitelist vs bridge handleMessage diff; three divergent dev-setup endpoints; auth/sessions+tokens routes possibly unmounted; `workflow:*` vs `multiagent:*` fork) — file as known-issues list in a `cerberus-docs/technical/` note when the session closes.
5. **M2** mixed ACP+PTY capability-matched scheduling — only after L2 is stable.

At session close, write these into the closing memory entry with pointers.

---

## Self-Review (done at plan time)

- Spec coverage: fidelity manifest (Tasks 1-2), generic harness + cross-actor templating (Task 3), L1/M1/L2 ladder (Tasks 5-7), emulated track explicitly deferred (Task 8). The watermark requirement from the spec is Task 2. ✓
- Placeholders: two intentional verify-at-runtime probes (Task 4 Step 2 cliType/command mapping; Task 6 Step 2 D1 schema) are investigation steps with recorded outputs, not deferred work.
- Type consistency: `Fidelity`/`ProcessSpec`/`harness.LaunchActor`/`PathParams` merge point consistent across tasks; `{{env.NAME}}` extension is called out as conditional in Task 7 with its own unit test if needed.

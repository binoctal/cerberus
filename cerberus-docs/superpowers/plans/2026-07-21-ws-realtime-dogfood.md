# WS Realtime Tier-1 Dogfood — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a minimal self-authored WebSocket target (login + realtime) plus a `protocol_ref` cerberus project, then run `cerberus run` against it live to validate the M0–M3-1 engine end-to-end and collect M3-2/M3-3 dogfood signals.

**Architecture:** A standalone in-repo Go server (`dogfood/ws-realtime/`) serves `POST /login` (issues a token) and `WS /realtime` (validates it, sends `devices:sync`, replies `device:ack`). A self-contained `.cerberus/` project declares the service via `protocol_ref`. Tasks 1–3 build the harness with TDD on a feature branch; Task 4 is the live LLM dogfood run (main session, observational) that produces the findings doc.

**Tech Stack:** Go 1.25, `github.com/coder/websocket` v1.8.14, `net/http`, `encoding/json`, cerberus `internal/project` loader.

## Global Constraints

- Go 1.25; module `github.com/binoctal/cerberus`. No CGo.
- WebSocket library is fixed to `github.com/coder/websocket` v1.8.14; `nhooyr.io/websocket` is forbidden; no expression/JSONPath/evaluator dependencies.
- Commit author MUST be `binoctal <binoctal@gmail.com>`; no `Co-Authored-By`. Code comments and commit messages in English.
- ALL documentation under `cerberus-docs/` only; NEVER `docs/`.
- `make check` (fmt + lint + test -race) must be green; tests are table-driven where multiple cases exist.
- Do NOT suggest CI / workflows / automation (user opted out).
- Execute Tasks 1–3 on branch `feat/ws-realtime-dogfood` (created at execution time). Spec/plan docs are already on `main`; only harness code + the findings doc are new.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-realtime-dogfood-design.md`

---

## File Structure

- **Create** `dogfood/ws-realtime/main.go` — the login + WS target server (`package main`). One file, one responsibility: serve the dogfood target.
- **Create** `dogfood/ws-realtime/main_test.go` — table-driven tests for the server (login, WS flow) + a loader test for the project config.
- **Create** `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml` — standalone protocol declaration (`protocol_ref` target; committable).
- **Create** `dogfood/ws-realtime/.cerberus/project.yaml` — service + actor (with `auth:`) + settings.
- **Create** `cerberus-docs/technical/dogfood/2026-07-21-ws-realtime-dogfood.md` — findings doc (Task 4, retro format).

`dogfood/ws-realtime/.cerberus/runtime/` is created at run time by cerberus and is gitignored (matches `.cerberus/runtime/` rule). `baseDir` for `protocol_ref` resolution is the `project.yaml` directory (`dogfood/ws-realtime/.cerberus/`), so `protocol_ref: open-agents` resolves to `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`.

---

## Task 1: Server scaffolding + `/login` endpoint

**Files:**
- Create: `dogfood/ws-realtime/main.go`
- Create: `dogfood/ws-realtime/main_test.go`

**Interfaces:**
- Produces: `newServer() *server`, `(*server) handleLogin`, `(*server) routes() http.Handler` (login route only in this task; WS routes added in Task 2).

- [ ] **Step 1: Write the failing test**

Create `dogfood/ws-realtime/main_test.go`:

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServer_Login(t *testing.T) {
	srv := httptest.NewServer(newServer().routes())
	defer srv.Close()

	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
		wantToken  bool
	}{
		{name: "valid creds", body: `{"email":"web@dogfood.local","password":"dogfood-web"}`, wantStatus: http.StatusOK, wantToken: true},
		{name: "missing creds", body: `{"email":"","password":""}`, wantStatus: http.StatusUnauthorized, wantToken: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/login", "application/json", strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status=%d want %d", resp.StatusCode, tc.wantStatus)
			}
			var got struct {
				Token string `json:"token"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&got)
			if tc.wantToken && got.Token == "" {
				t.Fatal("expected non-empty token")
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./dogfood/ws-realtime/ -run TestServer_Login -v`
Expected: FAIL / build error — `newServer` and `routes` undefined (main.go does not exist yet).

- [ ] **Step 3: Write minimal implementation**

Create `dogfood/ws-realtime/main.go`:

```go
// Package main is a minimal WebSocket target for the cerberus WS-realtime
// dogfood. It mirrors open-agents' shape: an HTTP login endpoint issues a
// token, and a WebSocket endpoint validates it. See
// cerberus-docs/superpowers/specs/2026-07-21-ws-realtime-dogfood-design.md.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"
)

// server is the in-memory dogfood target. Tokens issued by /login are held
// here and validated on WS connect. No persistence; loose validation only.
type server struct {
	mu     sync.Mutex
	tokens map[string]bool
	next   int
}

func newServer() *server {
	return &server{tokens: make(map[string]bool)}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleLogin issues a token for any non-empty credentials. The dogfood does
// not authenticate the password; it only needs a round-trippable token for
// the executor's auth_flow -> rawToken -> WS query injection chain.
func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" {
		http.Error(w, "email and password required", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	s.next++
	tok := fmt.Sprintf("tok-%d", s.next)
	s.tokens[tok] = true
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"token": tok})
}

// routes wires the HTTP endpoints. Task 1 registers only /login; Task 2 adds
// the WebSocket /realtime (and lenient /) routes.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.handleLogin)
	return mux
}

func main() {
	addr := flag.String("addr", ":8787", "listen address")
	flag.Parse()
	log.Printf("ws-realtime dogfood target listening on %s (POST /login, WS /realtime)", *addr)
	log.Fatal(http.ListenAndServe(*addr, newServer().routes()))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./dogfood/ws-realtime/ -run TestServer_Login -v`
Expected: PASS — both subtests pass.

- [ ] **Step 5: Commit**

```bash
git add dogfood/ws-realtime/main.go dogfood/ws-realtime/main_test.go
git commit -m "feat(ws-dogfood): add login endpoint to minimal WS target"
```

---

## Task 2: WebSocket `/realtime` endpoint

**Files:**
- Modify: `dogfood/ws-realtime/main.go` (add `handleWS`; register `/realtime` and `/` in `routes()`)
- Modify: `dogfood/ws-realtime/main_test.go` (append `TestServer_Realtime` + helpers; add imports)

**Interfaces:**
- Consumes: `(*server) tokens`, `(*server) routes()` from Task 1.
- Produces: `(*server) handleWS` wired into `routes()`.

- [ ] **Step 1: Write the failing test**

Append to `dogfood/ws-realtime/main_test.go`, and add these imports to the existing import block: `"context"`, `"fmt"`, `"time"`, and `"github.com/coder/websocket"`:

```go
func TestServer_Realtime(t *testing.T) {
	srv := httptest.NewServer(newServer().routes())
	defer srv.Close()

	tok := login(t, srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Invalid token: Accept always completes the WS handshake (101); the
	// handler closes post-upgrade, so Dial succeeds and the first Read fails.
	bad, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/realtime", "bogus", "web"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, _, err := bad.Read(ctx); err == nil {
		t.Fatal("expected read error after invalid-token close")
	}
	bad.CloseNow()

	// Valid token: devices:sync on connect, then device:command -> device:ack.
	c, _, err := websocket.Dial(ctx, wsURL(srv.URL, "/realtime", tok, "web"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	if _, data, err := c.Read(ctx); err != nil {
		t.Fatal(err)
	} else if msgType(data) != "devices:sync" {
		t.Fatalf("first msg=%s want devices:sync", data)
	}
	if err := c.Write(ctx, websocket.MessageText, []byte(`{"type":"device:command"}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ack ackShape
	if err := json.Unmarshal(data, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Type != "device:ack" || !ack.Payload.Approved || ack.Payload.Role != "web" {
		t.Fatalf("ack=%s", data)
	}
}

// login posts valid credentials and returns the issued token.
func login(t *testing.T, base string) string {
	t.Helper()
	resp, err := http.Post(base+"/login", "application/json", strings.NewReader(`{"email":"web@dogfood.local","password":"dogfood-web"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status %d", resp.StatusCode)
	}
	var got struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	return got.Token
}

// wsURL builds a ws:// URL with token and type query params.
func wsURL(base, path, token, role string) string {
	u := strings.Replace(base, "http://", "ws://", 1) + path
	return fmt.Sprintf("%s?token=%s&type=%s", u, token, role)
}

func msgType(data []byte) string {
	var m struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &m)
	return m.Type
}

type ackShape struct {
	Type    string `json:"type"`
	Payload struct {
		Approved bool   `json:"approved"`
		Role     string `json:"role"`
	} `json:"payload"`
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./dogfood/ws-realtime/ -run TestServer_Realtime -v`
Expected: FAIL — `handleWS` undefined / `/realtime` not registered (404 → dial fails or first read fails).

- [ ] **Step 3: Write minimal implementation**

In `dogfood/ws-realtime/main.go`, add `"context"` and `"github.com/coder/websocket"` to the import block, add the `handleWS` method, and extend `routes()`:

```go
// handleWS accepts a WebSocket, validates the ?token= query against issued
// tokens, sends devices:sync unconditionally, then replies device:ack to any
// device:command. ?type= flavors the ack role (default "web").
func (s *server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	s.mu.Lock()
	ok := s.tokens[r.URL.Query().Get("token")]
	s.mu.Unlock()
	if !ok {
		_ = c.Close(websocket.StatusPolicyViolation, "invalid token")
		return
	}
	role := r.URL.Query().Get("type")
	if role == "" {
		role = "web"
	}

	ctx := r.Context()
	syncMsg, _ := json.Marshal(map[string]any{"type": "devices:sync", "devices": []any{}})
	if err := c.Write(ctx, websocket.MessageText, syncMsg); err != nil {
		return
	}
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg["type"] == "device:command" {
			ack, _ := json.Marshal(map[string]any{
				"type":    "device:ack",
				"payload": map[string]any{"approved": true, "role": role},
			})
			if err := c.Write(ctx, websocket.MessageText, ack); err != nil {
				return
			}
		}
	}
}
```

Replace `routes()` with:

```go
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("/realtime", s.handleWS)
	mux.HandleFunc("/", s.handleWS) // lenient: accept WS upgrade at root too
	return mux
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./dogfood/ws-realtime/ -v`
Expected: PASS — `TestServer_Login` and `TestServer_Realtime` both pass.

- [ ] **Step 5: Commit**

```bash
git add dogfood/ws-realtime/main.go dogfood/ws-realtime/main_test.go
git commit -m "feat(ws-dogfood): add realtime WS endpoint with token gate and handshake"
```

---

## Task 3: Protocol declaration + project config + loader test

**Files:**
- Create: `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`
- Create: `dogfood/ws-realtime/.cerberus/project.yaml`
- Modify: `dogfood/ws-realtime/main_test.go` (append `TestProjectConfig_Loads`; add import `github.com/binoctal/cerberus/internal/project`)

**Interfaces:**
- Consumes: `project.LoadFromFile` (path → baseDir-aware load → `resolveProtocolRefs` → `Validate`).

- [ ] **Step 1: Write the failing test**

Append to `dogfood/ws-realtime/main_test.go` (add `"github.com/binoctal/cerberus/internal/project"` to imports):

```go
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
	if svc.Protocol.Auth == nil || svc.Protocol.Auth.Param != "token" || svc.Protocol.Auth.CredentialRef != "web-actor" {
		t.Fatalf("auth=%+v", svc.Protocol.Auth)
	}
	web := svc.Protocol.Roles["web"]
	if web == nil || web.Handshake == nil || web.Handshake.AwaitType != "devices:sync" {
		t.Fatalf("web role/handshake=%+v", web)
	}
	if len(cfg.Actors) != 1 || cfg.Actors[0].Name != "web-actor" {
		t.Fatalf("actors=%+v", cfg.Actors)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./dogfood/ws-realtime/ -run TestProjectConfig_Loads -v`
Expected: FAIL — `project.yaml` does not exist (open .cerberus/project.yaml: no such file).

- [ ] **Step 3: Write minimal implementation**

Create `dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml`:

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web:
    credential_ref: web-actor
    params:
      type: web
    handshake:
      await_type: devices:sync
      timeout: 5
```

Create `dogfood/ws-realtime/.cerberus/project.yaml`:

```yaml
project:
  name: ws-realtime-dogfood
services:
  - name: realtime
    url: http://localhost:8787
    protocol_ref: open-agents
actors:
  - name: web-actor
    credentials:
      email: web@dogfood.local
      password: dogfood-web
    auth:
      login:
        method: POST
        path: /login
        body: { email: "{email}", password: "{password}" }
      token_from: token
      inject_as: "Authorization: Bearer {token}"
settings:
  max_duration: 8m
  confidence_threshold: 0.7
  ai_budget:
    session_total_tokens: 60000
    per_call_limit: 10000
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./dogfood/ws-realtime/ -run TestProjectConfig_Loads -v`
Expected: PASS — config loads, `protocol_ref` resolves into `svc.Protocol`, web role + handshake present, actor present. (If `Validate` rejects, fix the offending field and re-run; the test enforces a valid config.)

- [ ] **Step 5: Run full check**

Run: `make check`
Expected: green (fmt + lint + test -race). The new `dogfood/ws-realtime` package compiles cleanly with no new external dependencies (`coder/websocket` already in go.mod).

- [ ] **Step 6: Commit**

```bash
git add dogfood/ws-realtime/.cerberus/protocols/open-agents.yaml dogfood/ws-realtime/.cerberus/project.yaml dogfood/ws-realtime/main_test.go
git commit -m "feat(ws-dogfood): add protocol_ref declaration and project config"
```

---

## Task 4: Live dogfood run + findings capture

**This task is observational, not TDD.** It requires the live LLM (creds inherited from `.claude/settings.json`) and two terminals. It is performed by the main session, not a dispatched subagent. Run it after Tasks 1–3 are ff-merged to `main`.

**Files:**
- Create: `cerberus-docs/technical/dogfood/2026-07-21-ws-realtime-dogfood.md`
- Update: cccmemory (decision/experimentation entries)

**Prerequisite:** Tasks 1–3 merged to `main` via the standard cycle (opus final review → ff-merge → `make check` → delete branch → update roadmap memory + `.superpowers/sdd/progress.md`).

- [ ] **Step 1: Build cerberus and start the target**

Terminal 1 (target):
```bash
make build
go run ./dogfood/ws-realtime
# → "ws-realtime dogfood target listening on :8787 (POST /login, WS /realtime)"
```

- [ ] **Step 2: Primary run (M1 fallback path)**

Terminal 2 (cerberus):
```bash
./build/cerberus run \
  --config dogfood/ws-realtime/.cerberus/project.yaml \
  --dir dogfood/ws-realtime \
  --goal "As a web client, connect to the realtime service WebSocket at /realtime, send a {type: device:command} message, and verify the server replies with a {type: device:ack} whose payload.approved is true."
```

Observe and record (from the run logs / session report):
- Did Scout plan a case targeting the `realtime` service?
- Did the Steer LLM emit `ws_connect` / `ws_send` / `ws_receive` at all?
- If the LLM **never emits `ws_*`** → **Finding #0** (the verified prompt defect: `prompts.go:7,43` omit `ws_*` from the action-type enum). Proceed to Step 3; otherwise skip to Step 4.

- [ ] **Step 3: (Conditional) Fix Finding #0, then re-run**

Only if Finding #0 fired. On a new branch `feat/ws-realtime-engine-dogfood-promptenum`:
- Edit `internal/head/agent/prompts.go`: add `ws_connect, ws_send, ws_receive, ws_disconnect` to the RULES action-type list (line 7) and to the output schema `action.type` enum (line 43). Keep the single raw-string literal intact (inline edit, no backticks/concatenation).
- `make check` green.
- opus whole-branch review → ff-merge → delete branch.
- Re-run Step 2 and confirm the LLM now emits `ws_*` before drawing engine conclusions.

- [ ] **Step 4: Secondary run (M2 path, goal-hinted)**

Same config, different goal — feeds the role name + handshake hint that role discovery (unsolved) cannot provide:
```bash
./build/cerberus run \
  --config dogfood/ws-realtime/.cerberus/project.yaml \
  --dir dogfood/ws-realtime \
  --goal "As a web client, connect with role 'web' (the server auto-completes a devices:sync handshake after connect), send {type: device:command}, and verify {type: device:ack} with payload.approved true."
```
Observe: does the engine expand the role (inject `type=web`, auto-await `devices:sync`)? Does the Steer LLM respect the "handshake runs automatically" hint and not re-receive it?

- [ ] **Step 5: Drift runs (M3-2 signal)**

Repeat the Step 2 goal 2–3 times. **Use a fresh runtime DB per run** so reflexion recall does not confound drift:
```bash
rm -rf dogfood/ws-realtime/.cerberus/runtime/
# re-run Step 2
```
Diff the per-run Steer action sequences (action types in order, from the run logs / session report). Record whether the sequence is stable or varies run-to-run.

- [ ] **Step 6: Write the findings doc**

Create `cerberus-docs/technical/dogfood/2026-07-21-ws-realtime-dogfood.md` following the established retro format (`Setup → Findings (problem/root-cause/resolution) → Outcome (verdict/token/time) → Resolution (fixed / not-a-bug / known-limitation) → Confirmed`). Cover at minimum:
- Engine mechanics that worked / broke through the full Scout→Agent(Steer)→Examiner pipeline.
- Finding #0 if fired (prompt enum) and its resolution.
- Role-discovery gap (M3-2 trigger): roles unusable without goal-hinting.
- Drift observation (M3-2 signal): stable vs varying Steer sequences.
- Blank-page cost of hand-authoring `open-agents.yaml` (M3-3 signal).
- `protocol_ref` real-run loading (M3-1 deferred integration gap closed or not).

- [ ] **Step 7: Record key findings in cccmemory**

Write the dogfood outcome to the `ws-realtime-engine-roadmap` memory (decision/experimentation): engine validation result, whether M3-2 / M3-3 triggers fired, Finding #0 status. Update `.superpowers/sdd/progress.md` with a dogfood section.

- [ ] **Step 8: Commit findings**

```bash
git add cerberus-docs/technical/dogfood/2026-07-21-ws-realtime-dogfood.md
git commit -m "docs(ws): record Tier-1 WS realtime dogfood findings"
```

---

## Self-Review

**1. Spec coverage:**
- D1 staged target → whole plan (Tier-1). ✓
- D2 representative open-agents-like (login + WS) → Tasks 1–2. ✓
- D3 server serves login → Task 1. ✓
- D4 `protocol_ref` → Task 3 + asserted by `TestProjectConfig_Loads`. ✓
- D5 in-repo standalone → `dogfood/ws-realtime/`. ✓
- D6 single web role → protocol yaml (no bridge). ✓
- D7 two run paths + run-as-is → Task 4 steps 2/4 + conditional step 3. ✓
- Observations/signal mapping → Task 4 steps 6/7. ✓
- Risks R1 (prompt enum) → Task 4 step 3; R2 (roles) → step 4; R5 (reflexion) → step 5. ✓

**2. Placeholder scan:** none. Every code/test step shows complete code; commands show expected output.

**3. Type consistency:** `newServer`, `routes()`, `handleLogin`, `handleWS` names match across tasks and tests. Loader-test field names (`Protocol.Framing`, `Protocol.Auth.Param`, `Protocol.Auth.CredentialRef`, `Protocol.Roles["web"].Handshake.AwaitType`, `Actors[].Name`) verified against `internal/project/protocol_schema.go` and `schema.go`. Test helpers (`login`, `wsURL`, `msgType`, `ackShape`) defined in Task 2 where first used.

**Note:** `AIBudget.Model` is intentionally omitted from `project.yaml` — `Validate` does not require it, and `resolveModel` inherits it from `.claude/settings.json` at run time (matches the 2026-07-18 self-dogfood).

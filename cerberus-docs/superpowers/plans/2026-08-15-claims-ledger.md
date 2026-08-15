# Claims Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A per-project claims ledger that reconciles the SUT's own documented promises against test evidence (with fidelity tiering) and hard-gates `cerberus run` (exit 3) while any critical claim is unproven.

**Architecture:** New `Claims` declaration file (`.cerberus/claims.yaml`) loaded alongside project config; `TestCase.Claims` binds cases to claims; a post-Examiner `reconcileClaims` session step computes per-claim status (proven / emulated-only / unevidenced) using the fidelity manifest and harness-captured real-actor identities; an LLM extraction subcommand (auto-run when the ledger is absent) keeps the ledger populated with zero human steps.

**Tech Stack:** Go 1.25, module `github.com/binoctal/cerberus`, cobra CLI, existing Scout LLM driver.

**Spec:** `cerberus-docs/superpowers/specs/2026-08-15-claims-ledger-design.md`

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By.
- Comments/commits in English; no CGo; docs only under `cerberus-docs/`.
- Branch `feat/claims-ledger` (already created). Tests `go test -race ./...`; lint via `make lint`.
- Gate semantics (spec, verbatim): critical claim not `proven` (and without a `wont-test(...)` annotation) ⇒ session status `incomplete`, `cerberus run` exits **3**.
- Evidence tier (spec, verbatim): `real` = case connects as a real-process role OR its send payloads (after placeholder resolution) reference a real-process actor's captured identity; `emulated` otherwise.
- Extraction merge mirrors vocab re-extraction: preserve `status_annotation`/`critical` on existing ids, append-only, deletion only with `--prune`.

---

### Task 1: Claims schema, loader, validation

**Files:**
- Create: `internal/project/claims.go`
- Test: `internal/project/claims_test.go`
- Modify: `internal/project/loader.go` (load alongside config)

**Interfaces:**
- Produces:

```go
// Claim is one falsifiable product promise from the SUT's own docs.
type Claim struct {
	ID               string `yaml:"id"`
	Text             string `yaml:"text"`
	SourceRef        string `yaml:"source_ref,omitempty"`
	Critical         bool   `yaml:"critical,omitempty"`
	ImpliesCardinality int  `yaml:"implies_cardinality,omitempty"`
	StatusAnnotation string `yaml:"status_annotation,omitempty"`
}

// ClaimsFile is the .cerberus/claims.yaml document.
type ClaimsFile struct {
	Source struct {
		Files []struct {
			Path string `yaml:"path"`
			Hash string `yaml:"hash,omitempty"`
		} `yaml:"files"`
	} `yaml:"source"`
	Claims []Claim `yaml:"claims"`
}

// LoadClaims reads .cerberus/claims.yaml; returns (nil, nil) when absent.
func LoadClaims(projectDir string) (*ClaimsFile, error)

// ValidateClaims: unique kebab-case ids, non-empty text, wont-test
// annotations must carry a reason: "wont-test(<reason>)".
func ValidateClaims(cf *ClaimsFile) error

// WontTest reports whether the annotation exempts the claim from the gate.
func (c Claim) WontTest() bool
```

- `project.Config` gains `Claims *ClaimsFile` (`yaml:"-"`), loaded by the same loader pass that resolves protocol refs (loader.go:102-140 area); absence is not an error.

- [x] **Step 1: failing tests** — parse a YAML sample (fields round-trip), duplicate id rejected, `id_Invalid` rejected, `wont-test()` without reason rejected, `LoadClaims` on a temp dir with/without the file, `WontTest()` true for `wont-test(no surface)` / false for `""` and `maybe later`.
- [x] **Step 2: run** `go test ./internal/project/ -run Claims -v` — FAIL (undefined).
- [x] **Step 3: implement** claims.go + loader hook.
- [x] **Step 4: run** `go test ./internal/project/ -race` — PASS.
- [x] **Step 5: Commit:** `git commit -m "feat(project): claims ledger schema, loader, validation"`

### Task 2: TestCase.Claims + repair-case inheritance

**Files:**
- Modify: `internal/head/agent/types.go` (TestCase struct, next to FallbackFor/Replaces)
- Modify: `internal/session/run_phases_repair.go` (inheritance at the repair-merge point, after `repairPlanFn` returns at run_phases_repair.go:270)
- Test: `internal/session/repair_claims_inheritance_test.go`

**Interfaces:**
- Produces: `TestCase.Claims []string \`json:"claims,omitempty"\``.
- Inheritance rule: for every repaired case with `Replaces != ""` or `FallbackFor != ""`, copy `Claims` from the original case (looked up by ID in `rp.plan.Cases`) when the new case has none.

- [x] **Step 1: failing test** — plan with case A `{ID:"A", Claims:["schedule-real-cli"]}`, repair output case `{ID:"A-r", Replaces:"A"}`; run the merge helper; assert `A-r.Claims == ["schedule-real-cli"]`. Test the helper as a pure function `inheritClaims(newCases []agent.TestCase, originals []agent.TestCase) []agent.TestCase` living in run_phases_repair.go.
- [x] **Step 2: run** — FAIL.
- [x] **Step 3: implement** field + helper + call it where repaired cases join the plan.
- [x] **Step 4: run** `go test ./internal/session/ ./internal/head/agent/ -race` — PASS.
- [x] **Step 5: Commit:** `git commit -m "feat(agent): TestCase.Claims + repair-case claim inheritance"`

### Task 3: Reconciliation core (pure)

**Files:**
- Create: `internal/session/claims_reconcile.go`
- Test: `internal/session/claims_reconcile_test.go`

**Interfaces:**
- Consumes: Task 1 `Claim`, Task 2 `TestCase.Claims`, fidelity manifest (`project.FidelityRealProcess`), session actor state.
- Produces:

```go
type ClaimStatus string

const (
	ClaimProven       ClaimStatus = "proven"
	ClaimEmulatedOnly ClaimStatus = "emulated-only"
	ClaimUnevidenced  ClaimStatus = "unevidenced"
)

type ClaimVerdict struct {
	Claim  project.Claim
	Status ClaimStatus
	Cases  []string // bound case ids (passing first)
}

// caseEvidenceTier reports the best tier a PASSED case's evidence reaches.
// real: the case connects as a real-process role, or a send body contains
// any value from realActorIds (harness-captured deviceId etc. — compare
// against the RAW message string after {{...}} resolution is unavailable
// here, so match both the raw body and the actor's path-param values).
func caseEvidenceTier(tc agent.TestCase, realRoleActors map[string]bool, realActorIds []string) (tier string)

// ReconcileClaims computes every claim's verdict.
// claims: ledger; results: final step results (verdict Status + TestCase);
// realRoleActors: actor names with fidelity real-process;
// realActorIds: their captured identity values present in the session.
func ReconcileClaims(claims []project.Claim, results []agent.StepResult, realRoleActors map[string]bool, realActorIds []string) []ClaimVerdict

// ClaimsGateFailed: any critical claim without WontTest whose status != proven.
func ClaimsGateFailed(verdicts []ClaimVerdict) bool
```

- [x] **Step 1: failing tests** — the full matrix:
  - passing case + connects as real role → proven
  - passing case + emulated + body contains `"deviceId":"device_x"` where device_x ∈ realActorIds → proven (spec amendment #1)
  - passing case + emulated + no reference → emulated-only
  - no bound cases → unevidenced; bound but failed → unevidenced
  - critical + emulated-only → gate failed; critical + wont-test → gate passes; non-critical + unevidenced → gate passes
  - repair-inherited case (Claims via Replaces path result) proves the claim
- [x] **Step 2: run** — FAIL. **Step 3: implement** (pure functions, no I/O).
- [x] **Step 4: run** `go test ./internal/session/ -race` — PASS.
- [x] **Step 5: Commit:** `git commit -m "feat(session): claims reconciliation core with fidelity tiering"`

### Task 4: Session wiring + summary + hard gate exit 3

**Files:**
- Modify: `internal/session/run_phases_lifecycle.go` (reconcile after buildSummary), `internal/session/resume_phases_helpers.go` (resume path)
- Modify: `internal/session/summary.go` (fields + String lines), `internal/session/lifecycle_run.go`
- Create: `internal/session/claims_gate.go` (sentinel error + collectRealIdentities helper)
- Modify: `cmd/cerberus/main_run.go:114-121` (translate sentinel → os.Exit(3))
- Test: `internal/session/claims_gate_test.go`, `internal/session/summary_test.go`

**Interfaces:**
- Produces: `var ErrClaimsGate = errors.New("claims gate: critical claims not proven")`; `SessionSummary.ClaimsProven/ClaimsEmulatedOnly/ClaimsUnevidenced int` + `ClaimsRedLines []string` (JSON `claims_*/claims_red_lines`); summary String appends `\n  Claims: N proven / N emulated-only / N unevidenced` and one `\n  UNRECONCILED: <id> — <text> (<status>)` per red line.
- `collectRealIdentities` walks `s.Config.Actors` for fidelity real-process: actor name → realRoleActors, and appends every value of `a.Credentials.PathParams` to realActorIds (harness captures deviceId there).

Wiring: in `buildSummary` (and resume buildSummary), after FidelityComposition: if `s.Config.Claims != nil && len(Claims) > 0` → verdicts := ReconcileClaims(...) using `rp.results`; stash verdicts on the summary; if `ClaimsGateFailed(verdicts)` → mark `rp.summary.ClaimsGateTriggered = true`. `Session.Run` returns `ErrClaimsGate` (after finalize, i.e. as the final error) when the flag is set. `main_run.go` checks `errors.Is(err, session.ErrClaimsGate)` → prints the summary already logged, `os.Exit(3)`.

- [x] **Step 1: failing tests** — summary rendering with the three counts + red lines; gate sentinel returned from a Run-shaped path (unit-test the helper `gateErrorIfFailed(summary) error`); exit translation unit: call the run cmd's error-mapping helper with the sentinel and assert exit code 3 path (factor the mapping into `mapRunExitError(err) int` in main_run.go).
- [x] **Step 2: run** — FAIL. **Step 3: implement** all wiring.
- [x] **Step 4: run** `go test ./internal/session/ ./cmd/... -race` — PASS. **Step 5: Commit:** `git commit -m "feat(session): claims reconciliation wiring + hard gate (exit 3)"`

### Task 5: `cerberus claims extract` (LLM, auto-merge) + run auto-extract

**Files:**
- Create: `cmd/cerberus/main_claims.go` (extract/list/check subcommands)
- Create: `internal/claimsdiscover/extract.go` (+ `prompt.go`)
- Modify: `cmd/cerberus/main_run.go` (auto-extract when ledger absent)
- Test: `internal/claimsdiscover/extract_test.go` (merge logic with a fake LLM), `cmd/cerberus/main_claims_test.go`

**Interfaces:**
- Produces:

```go
// Extract calls the LLM on the joined doc text and returns draft claims.
// Prompt contract (prompt.go): only falsifiable capability claims; reject
// marketing adjectives; ≤ max (default 15); each with id (kebab-case),
// text, source_ref (file:line when identifiable), implies_cardinality
// (2+ when the claim text implies multiple instances/agents/devices).
func Extract(ctx context.Context, drv *ai.Driver, docs map[string]string, max int) ([]project.Claim, error)

// SurfaceTriage marks critical: true only for claims whose text matches a
// known surface token (service URL host, protocol role/message type, actor
// name, process command); others get critical:false + annotation
// "no surface mapping". Annotation only set when the claim is NEW.
func SurfaceTriage(draft []project.Claim, cfg *project.Config) []project.Claim

// MergeClaims appends new ids, preserves existing Critical and
// StatusAnnotation; prune removes ids absent from draft only when prune=true.
func MergeClaims(existing *project.ClaimsFile, draft []project.Claim, prune bool) *project.ClaimsFile
```

- CLI: `cerberus claims extract --from <path> [--max 15] [--prune]` writes `.cerberus/claims.yaml` (source hash like vocab: SHA-256 each file). `cerberus claims list` renders the ledger. `cerberus claims check` reconciles from the latest session in the store and prints verdicts.
- Run auto-extract: in main_run.go before `sess.Run`, if the project dir has no claims.yaml and a doc source exists (project README* under --dir, else the first service's repo README located like vocab source paths — probe `../README.md` relative to the service workdir conventions used by dogfood), run Extract+Triage+Merge silently; log one line `claims ledger extracted (N claims, M critical)`.
- LLM client pattern: follow `internal/protocoldiscover/infer.go` driver usage.

- [x] **Step 1: failing tests** — MergeClaims preserves annotations/critical on existing ids, appends new, prunes only with flag; SurfaceTriage marks `critical:true` when text contains an actor name or message type from cfg, else false+annotation; Extract parses the LLM's JSON payload (fake driver returning a canned JSON) and enforces max.
- [x] **Step 2: run** — FAIL. **Step 3: implement** (package + commands + auto-extract).
- [x] **Step 4: run** `go test ./internal/claimsdiscover/ ./cmd/... -race` — PASS.
- [x] **Step 5: Commit:** `git commit -m "feat(claims): LLM extraction with surface triage, auto-merge CLI, run auto-extract"`

### Task 6: dogfood ledgers + gate validation (live)

**Files:**
- Create: `dogfood/ws-realtime/.cerberus/claims.yaml` (hand-written this round — extraction is validated on the next SUT; entries below)
- Create: `dogfood/realtime-e2e/.cerberus/claims.yaml`

ws-realtime ledger (expected RED — the gate must bite):

```yaml
source:
  files:
    - path: ../../../open-agents/README.md
claims:
  - id: ws-relay-messaging
    text: "WebSocket 实时消息转发"
    critical: true
  - id: schedule-real-cli
    text: "调度真实 AI CLI 执行任务"
    critical: true
  - id: multi-device-orchestration
    text: "多设备任务编排与调度"
    critical: true
  - id: permission-approval
    text: "实时审批 AI 操作请求"
    critical: true
  - id: desktop-notify
    text: "桌面通知"
    critical: false
    status_annotation: "no surface mapping"
```

realtime-e2e ledger: same critical claims plus binding — its deterministic cases must carry `Claims`; since the L1/L2 proof lives in integration tests (outside `cerberus run`), bind the ws-relay claim to the run's cases and mark `schedule-real-cli` with `status_annotation: "wont-test(proven by TestRealBridge_L2 integration suite)"` — the documented exemption channel in action.

- [x] **Step 1: write both ledgers**; add `Claims: ["ws-relay-messaging"]` binding to the dogfood case generator output for ws-realtime relay cases (scout `ws_cases.go`: single const claim id bound to all ws cases of the service — thread through `wsCasesForService`).
- [x] **Step 2: live gate check (needs the api server on :8989 + GLM env, see memory `openagents-live-port-and-auth-gotchas`):** run `cerberus run` in `dogfood/ws-realtime` — EXPECT exit 3 with red lines for schedule-real-cli / multi-device-orchestration / permission-approval. Assert via the log grep: `Claims:` line present, `UNRECONCILED:` ≥ 3.
- [x] **Step 3: realtime-e2e ledger check:** `cerberus claims check` in that dir renders proven/exempt verdicts (no live run needed — reconcile from an empty/empty store is acceptable for the check command test; the live e2e run remains deferred from the previous session).
- [x] **Step 4: Commit:** `git commit -m "feat(dogfood): claims ledgers + relay-case claim binding; gate validated live"`

> **Execution notes (2026-08-15):** Live run exited 3 as expected. Summary: 41 pass / 0 fail / 1 skip; `Claims: 0 proven / 1 emulated-only / 4 unevidenced`; 4 UNRECONCILED red lines (ws-relay-messaging was emulated-only — the run self-played all actors, so the gate correctly refused emulated evidence for a critical claim). realtime-e2e ledger scoped to the two L1/L2 claims (ws-relay-messaging + wont-test-exempt schedule-real-cli): multi-device/permission/desktop claims stay in ws-realtime's ledger, which documents the gap; carrying them critical-unexempt in realtime-e2e would gate-fail that run for claims it never claims to prove.

### Task 7: Deferred hand-off

Recorded for the next round (no implementation): findings backflow (real-run errors auto-entering the ledger as suspected defects), `replicas` cardinality execution in the harness, static surface inventory generators (HTTP routes / process registries) — see the spec's Non-goals.

---

## Self-Review

- Spec coverage: schema/loader/validate (T1), binding+inheritance (T2), status semantics incl. amendments #1/#2 (T3), gate exit 3 + summary (T4), extraction/triage/merge/auto-run + CLI (T5), live validation incl. expected-red (T6), deferrals (T7). ✓
- Placeholders: T6 Step 1's generator binding is spelled (single claim id const); the auto-extract doc-source probe lists concrete fallbacks. ✓
- Type consistency: `Claim`/`ClaimsFile` (T1) used verbatim in T3/T5; `ClaimVerdict`/`ReconcileClaims`/`ClaimsGateFailed` (T3) consumed by T4; sentinel `ErrClaimsGate` named in both T4 and main_run translation. ✓

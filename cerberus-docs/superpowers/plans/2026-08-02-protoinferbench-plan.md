# protoinferbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A repeatable, env-gated Go benchmark (`tools/protoinferbench`) that runs `cerberus protocol infer --dry-run` N times against the `open-agents` target, scores each draft against a fixed ground truth per-structure, and judges PASS/FAIL against thresholds — replacing subjective 3-5-run eyeballing.

**Architecture:** One `package main` in `tools/protoinferbench/` (mirrors `tools/slowtest/`). All pure logic (`ParseDraft`, `classifyRun`, `Score`, `Aggregate`, `formatReport`) lives in `score.go` and is unit-tested with no I/O; all network/subprocess code lives in `main.go`'s `runBench()` and is structurally unreachable from `go test`, plus a `CERBERUS_BENCH=1` guard. Scoring reuses `*project.Protocol` from `internal/project`.

**Tech Stack:** Go 1.25, stdlib `flag`/`os/exec`/`net/http`, `gopkg.in/yaml.v3 v3.0.1` (existing dep), `github.com/binoctal/cerberus/internal/project` (existing).

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, pure-Go SQLite (no CGo) — irrelevant here but do not add any CGo dep.
- Commit author MUST be `binoctal <binoctal@gmail.com>`; NEVER add `Co-Authored-By` / `Co-authored-By`.
- Code comments and commit messages in English; user-facing answers in Chinese.
- `make check` (fmt + lint + `go test -race ./...`) MUST exit 0 and MUST NOT touch the network or call any LLM.
- Documentation only under `cerberus-docs/`; NEVER write under `docs/`.
- Follow existing comment density / naming idiom (see `tools/slowtest/main.go`, `internal/protocoldiscover/infer.go`).
- Do not mention CI (user opted out).

## File Structure

- `tools/protoinferbench/score.go` — pure: types, structure/threshold tables, `ParseDraft`, `classifyRun`, `Score`, `Aggregate`, `formatReport`. No I/O.
- `tools/protoinferbench/score_test.go` — table-driven unit tests for everything in `score.go`, plus `testdata/` golden anchors. No I/O.
- `tools/protoinferbench/testdata/run2-draft.yaml` — real Run-2 draft YAML (envelope+roles+auth only), committed verbatim from the dogfood doc.
- `tools/protoinferbench/testdata/run22-like-draft.yaml` — reconstructed full-coverage fixture (handshake `devices:sync` + correct batch keys, `items_path: batch.lines`), labeled as a reconstruction.
- `tools/protoinferbench/main.go` — `package main` entry: flag parsing, `CERBERUS_BENCH` guard, health check, N-times `exec.Command` loop, prints `formatReport` output.

---

## Task 1: Pure scoring engine (score.go) with full unit tests

**Files:**
- Create: `tools/protoinferbench/score.go`
- Create: `tools/protoinferbench/score_test.go`
- Create: `tools/protoinferbench/testdata/run2-draft.yaml`
- Create: `tools/protoinferbench/testdata/run22-like-draft.yaml`

**Interfaces:**
- Consumes: `github.com/binoctal/cerberus/internal/project` (`*project.Protocol`, `*ProtocolAuth`, `*ProtocolRole`, `*RoleHandshake`, `*ProtocolBatch` — all defined in `internal/project/protocol_schema.go`).
- Produces (used by Task 2): `runResult`, `runOutcome` constants, `classifyRun(stdout, stderr string, exitCode int) runResult`, `Aggregate(results []runResult, samplesHint string) report`, and the `report` type whose `String()` / a `formatReport`-equivalent renders the markdown table.

- [ ] **Step 1: Create testdata fixtures**

Create `tools/protoinferbench/testdata/run2-draft.yaml` (verbatim Run-2 draft from the dogfood doc — envelope + roles + auth, no handshake/batch):

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: ""
roles:
  bridge:
    credential_ref: ""
    params:
      deviceId: '{{deviceId}}'
      type: bridge
  web:
    credential_ref: ""
    params:
      type: web
```

Create `tools/protoinferbench/testdata/run22-like-draft.yaml` — a reconstruction (NOT a literal run; assembled from the dogfood Run-22 table row: handshake `devices:sync` optional on web, correct batch flush key + item_type, `items_path` off as `batch.lines`). Header comment states it is reconstructed:

```yaml
# Reconstructed from the Run-22 dogfood table row (not a literal run capture):
# handshake devices:sync (web, optional=true); batch session:output-batch /
# session:output; items_path batch.lines (imprecise vs payload.lines).
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: ""
roles:
  web:
    credential_ref: ""
    params:
      type: web
    handshake:
      await_type: devices:sync
      timeout: 5000
      optional: true
batches:
  session:output-batch:
    item_type: session:output
    items_path: batch.lines
```

- [ ] **Step 2: Write the failing test file**

Create `tools/protoinferbench/score_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

func mustLoad(t *testing.T, name string) *project.Protocol {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var p project.Protocol
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return &p
}

func TestScore(t *testing.T) {
	run2 := mustLoad(t, "run2-draft.yaml")
	run22 := mustLoad(t, "run22-like-draft.yaml")
	// run2: envelope + roles + auth only.
	wantRun2 := [numStructures]bool{true, true, true, true, false, false, false}
	// run22: all but batch_items_path (batch.lines != payload.lines).
	wantRun22 := [numStructures]bool{true, true, true, true, true, true, false}

	for _, tc := range []struct {
		name string
		p    *project.Protocol
		want [numStructures]bool
	}{
		{"run2", run2, wantRun2},
		{"run22", run22, wantRun22},
		{"nil", nil, [numStructures]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(tc.p)
			if got != tc.want {
				t.Fatalf("Score(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestParseDraft(t *testing.T) {
	t.Run("valid prefixed", func(t *testing.T) {
		raw := "Draft protocol \"open-agents\":\nframing: json\ntype_path: type\n"
		p, err := ParseDraft(raw)
		if err != nil || p == nil {
			t.Fatalf("got (%p, %v), want parsed proto", p, err)
		}
		if p.Framing != "json" || p.TypePath != "type" {
			t.Fatalf("parsed fields wrong: %+v", p)
		}
	})
	t.Run("no draft line", func(t *testing.T) {
		if _, err := ParseDraft("no WebSocket protocol found in the provided inputs\n"); err != errNoDraft {
			t.Fatalf("got %v, want errNoDraft", err)
		}
	})
	t.Run("corrupt yaml", func(t *testing.T) {
		raw := "Draft protocol \"x\":\nframing: [unclosed\n"
		if _, err := ParseDraft(raw); err == nil {
			t.Fatalf("want parse error, got nil")
		}
	})
}

func TestClassifyRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stdout   string
		exitCode int
		outcome  runOutcome
		hasProto bool
	}{
		{"draft", "Draft protocol \"x\":\nframing: json\n", 0, outcomeDraft, true},
		{"no_protocol", "no WebSocket protocol found in the provided inputs\n", 0, outcomeNoProtocol, false},
		{"parse_fail", "Draft protocol \"x\":\nframing: [bad\n", 0, outcomeParseFail, false},
		{"hard_error", "Error: model produced an invalid protocol\n", 1, outcomeHardError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := classifyRun(tc.stdout, "stderr", tc.exitCode)
			if r.outcome != tc.outcome {
				t.Fatalf("outcome = %s, want %s", r.outcome, tc.outcome)
			}
			if r.hasProto() != tc.hasProto {
				t.Fatalf("hasProto = %v, want %v", r.hasProto(), tc.hasProto)
			}
		})
	}
}

func TestAggregate(t *testing.T) {
	good := mustLoad(t, "run22-like-draft.yaml")
	// 4 runs: 2 good drafts, 1 no_protocol, 1 hard_error -> N=4.
	results := []runResult{
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeNoProtocol},
		{outcome: outcomeHardError},
	}
	rep := Aggregate(results, "3")
	if rep.n != 4 {
		t.Fatalf("n = %d, want 4", rep.n)
	}
	// run22 hits 6/7 structures; over 2 good drafts -> 2 hits each, denom 4.
	wantHits := map[string]int{
		"framing": 2, "type_path": 2, "auth": 2, "roles": 2,
		"handshake": 2, "batch_keys": 2, "batch_items_path": 0,
	}
	got := map[string]int{}
	for _, s := range rep.structures {
		got[s.name] = s.hits
	}
	for name, want := range wantHits {
		if got[name] != want {
			t.Fatalf("hits[%s] = %d, want %d", name, got[name], want)
		}
	}
	// batch_items_path: 0/4 = 0% < 40% threshold -> overall FAIL.
	if rep.overall {
		t.Fatalf("overall = PASS, want FAIL (batch_items_path below threshold)")
	}
}

func TestFormatReport(t *testing.T) {
	good := mustLoad(t, "run22-like-draft.yaml")
	results := []runResult{
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeNoProtocol},
		{outcome: outcomeHardError},
	}
	rep := Aggregate(results, "3")
	out := formatReport(rep)
	for _, want := range []string{
		"N=4", "samples=3 per run", "| Structure",
		"framing", "batch_items_path", "Overall:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q; got:\n%s", want, out)
		}
	}
}
```

- [ ] **Step 3: Run tests to verify they fail (compile errors)**

Run: `go test ./tools/protoinferbench/`
Expected: FAIL — `numStructures`, `Score`, `ParseDraft`, `classifyRun`, `runResult`, `runOutcome`, `errNoDraft`, `Aggregate`, `formatReport`, `report` undefined.

- [ ] **Step 4: Implement score.go**

Create `tools/protoinferbench/score.go`:

```go
// Command protoinferbench runs `cerberus protocol infer` N times against a live
// target, scores each draft against a fixed ground truth per-structure, and
// reports per-structure hit rates with threshold-gated PASS/FAIL. It exists to
// kill the subjective "looks right over 3-5 runs" loop: variance dominates small
// samples, so we run >=15, score structures objectively, and judge by thresholds.
//
// The benchmark is env-gated: it only touches the network when CERBERUS_BENCH=1.
// All pure scoring logic lives here (no I/O) so `go test` is network-free.
package main

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

// Scored structure indices, in fixed reporting order. Add to the bottom only;
// the order is part of the report contract.
const (
	idxFraming = iota
	idxTypePath
	idxAuth
	idxRoles
	idxHandshake
	idxBatchKeys
	idxBatchItemsPath
	numStructures
)

// structureNames maps each index to its report label.
var structureNames = [numStructures]string{
	"framing", "type_path", "auth", "roles", "handshake",
	"batch_keys", "batch_items_path",
}

// thresholds are the minimum hit rate (0..1) for each structure to PASS.
var thresholds = [numStructures]float64{
	0.95, 0.95, 0.95, 0.90, 0.60, 0.50, 0.40,
}

// Ground-truth literals for the open-agents target.
const (
	gtFraming       = "json"
	gtTypePath      = "type"
	gtAuthStrategy  = "query"
	gtAuthParam     = "token"
	gtRoleWeb       = "web"
	gtRoleBridge    = "bridge"
	gtWebParamType  = "web"
	gtBridgePType   = "bridge"
	gtHandshakeType = "devices:sync"
	gtBatchKey      = "session:output-batch"
	gtBatchItemType = "session:output"
	gtBatchItemsPath = "payload.lines"
)

// runOutcome classifies what a single `protocol infer` invocation produced.
type runOutcome string

const (
	outcomeDraft      runOutcome = "draft"
	outcomeNoProtocol runOutcome = "no_protocol"
	outcomeParseFail  runOutcome = "parse_fail"
	outcomeHardError  runOutcome = "hard_error"
)

// runResult is the outcome of one benchmark run plus its parsed draft (nil for
// non-draft outcomes).
type runResult struct {
	outcome runOutcome
	proto   *project.Protocol
}

// hasProto reports whether the run yielded a usable draft.
func (r runResult) hasProto() bool { return r.proto != nil }

// draftPrefix is the stdout marker emitted by `protocol infer --dry-run`.
const draftPrefix = "Draft protocol "

// errNoDraft is returned by ParseDraft when stdout carries no draft marker.
var errNoDraft = errors.New("no draft in output")

// ParseDraft extracts the YAML block following the "Draft protocol" line and
// unmarshals it into a *project.Protocol. It returns errNoDraft when the marker
// is absent (e.g. the "no WebSocket protocol found" message).
func ParseDraft(stdout string) (*project.Protocol, error) {
	idx := strings.Index(stdout, draftPrefix)
	if idx < 0 {
		return nil, errNoDraft
	}
	// Drop the marker line; everything after it is the YAML body.
	body := stdout[idx:]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	var p project.Protocol
	if err := yaml.Unmarshal([]byte(body), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// classifyRun inspects subprocess output and maps it to a runResult. It never
// returns an error: every failure mode (no protocol, parse failure, non-zero
// exit) becomes a non-draft outcome so the run still counts in the denominator.
func classifyRun(stdout, _ string, exitCode int) runResult {
	if exitCode != 0 {
		return runResult{outcome: outcomeHardError}
	}
	if strings.Contains(stdout, "no WebSocket protocol found") {
		return runResult{outcome: outcomeNoProtocol}
	}
	p, err := ParseDraft(stdout)
	if err != nil {
		return runResult{outcome: outcomeParseFail}
	}
	return runResult{outcome: outcomeDraft, proto: p}
}

// Score evaluates one draft into a fixed-order hit vector. A nil proto (failed
// run) is an all-miss vector.
func Score(p *project.Protocol) [numStructures]bool {
	var h [numStructures]bool
	if p == nil {
		return h
	}
	h[idxFraming] = p.Framing == gtFraming
	h[idxTypePath] = p.TypePath == gtTypePath
	h[idxAuth] = p.Auth != nil && p.Auth.Strategy == gtAuthStrategy && p.Auth.Param == gtAuthParam

	web, hasWeb := p.Roles[gtRoleWeb]
	bridge, hasBridge := p.Roles[gtRoleBridge]
	h[idxRoles] = hasWeb && hasBridge &&
		web.Params[gtWebParamType] == gtWebParamType &&
		bridge.Params[gtBridgePType] == gtBridgePType

	h[idxHandshake] = hasWeb && web.Handshake != nil &&
		web.Handshake.AwaitType == gtHandshakeType && web.Handshake.Optional

	batch, hasBatch := p.Batches[gtBatchKey]
	h[idxBatchKeys] = hasBatch && batch.ItemType == gtBatchItemType
	h[idxBatchItemsPath] = hasBatch && batch.ItemsPath == gtBatchItemsPath
	return h
}

// structureStat is one row of the report.
type structureStat struct {
	name      string
	hits      int
	n         int
	threshold float64
}

// rate is the hit fraction in [0,1].
func (s structureStat) rate() float64 {
	if s.n == 0 {
		return 0
	}
	return float64(s.hits) / float64(s.n)
}

// report is the aggregated benchmark result.
type report struct {
	n           int
	samplesHint string
	structures  []structureStat
	outcomes    map[runOutcome]int
	overall     bool
}

// Aggregate tallies per-structure hits across results and applies thresholds.
// Failed runs (non-draft outcomes) contribute 0 hits but count in the
// denominator n, matching the brief's "pass-2 drift counts as not-hit".
func Aggregate(results []runResult, samplesHint string) report {
	rep := report{
		n:           len(results),
		samplesHint: samplesHint,
		structures:  make([]structureStat, numStructures),
		outcomes:    map[runOutcome]int{},
	}
	for i := 0; i < numStructures; i++ {
		rep.structures[i] = structureStat{
			name: structureNames[i], n: rep.n, threshold: thresholds[i],
		}
	}
	for _, r := range results {
		rep.outcomes[r.outcome]++
		h := Score(r.proto)
		for i := 0; i < numStructures; i++ {
			if h[i] {
				rep.structures[i].hits++
			}
		}
	}
	rep.overall = true
	for _, s := range rep.structures {
		if s.rate() < s.threshold {
			rep.overall = false
			break
		}
	}
	return rep
}
```

- [ ] **Step 5: Run Score/ParseDraft/classifyRun/Aggregate tests**

Run: `go test ./tools/protoinferbench/ -run 'TestScore|TestParseDraft|TestClassifyRun|TestAggregate' -v`
Expected: those four PASS. `TestFormatReport` still FAILs (`formatReport` undefined) — that is the next step.

- [ ] **Step 6: Implement formatReport**

Append to `tools/protoinferbench/score.go`:

```go
import (
	"fmt"
	// ... keep existing imports, add "fmt"
)

// formatReport renders the benchmark result as a markdown table pasteable into
// the dogfood doc.
func formatReport(rep report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Protocol infer benchmark — open-agents (N=%d, samples=%s per run)\n\n",
		rep.n, rep.samplesHint)
	fmt.Fprintf(&b, "Run outcomes:\n")
	// Stable order: draft, no_protocol, parse_fail, hard_error.
	for _, o := range []runOutcome{outcomeDraft, outcomeNoProtocol, outcomeParseFail, outcomeHardError} {
		fmt.Fprintf(&b, "  %-11s : %2d\n", o, rep.outcomes[o])
	}
	fmt.Fprintf(&b, "\nPer-structure hit rate:\n")
	fmt.Fprintf(&b, "| Structure        | Hits  | Rate  | Threshold | Result |\n")
	fmt.Fprintf(&b, "|------------------|-------|-------|-----------|--------|\n")
	passed := 0
	for _, s := range rep.structures {
		rate := s.rate()
		res := "FAIL"
		if rate >= s.threshold {
			res, passed = "PASS", passed+1
		}
		fmt.Fprintf(&b, "| %-16s | %d/%d | %3.0f%%  | >=%-2.0f%%     | %-6s |\n",
			s.name, s.hits, s.n, rate*100, s.threshold*100, res)
	}
	verdict := "FAIL"
	if rep.overall {
		verdict = "PASS"
	}
	fmt.Fprintf(&b, "\nOverall: %s (%d/7 structures)\n", verdict, passed)
	return b.String()
}
```

(Fold `fmt` into the single import block at the top of `score.go`; the snippet above is illustrative of the addition.)

- [ ] **Step 7: Run all score tests**

Run: `go test ./tools/protoinferbench/ -v`
Expected: all PASS (Score, ParseDraft, classifyRun, Aggregate, formatReport).

- [ ] **Step 8: Lint, fmt, commit**

Run: `make fmt && make lint && go test ./tools/protoinferbench/ -race`
Expected: clean; tests green.

```bash
git add tools/protoinferbench/score.go tools/protoinferbench/score_test.go tools/protoinferbench/testdata/
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit \
  -m "feat(protoinferbench): pure scoring engine (Score/ParseDraft/Aggregate/formatReport)"
```

---

## Task 2: Orchestration (main.go) — env-gated, health-checked, N-times run

**Files:**
- Create: `tools/protoinferbench/main.go`

**Interfaces:**
- Consumes (from Task 1): `classifyRun(stdout, stderr string, exitCode int) runResult`, `Aggregate(results []runResult, samplesHint string) report`, `formatReport(rep report) string`.
- Produces: a runnable `go run ./tools/protoinferbench` / built binary that prints the report. Nothing downstream (Task 3 runs it manually).

- [ ] **Step 1: Implement main.go**

Create `tools/protoinferbench/main.go`:

```go
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// benchEnv gates real LLM/subprocess work. Unset -> the tool prints a skip line
// and exits 0, so an accidental invocation under `go test` or `make check`
// cannot touch the network. The scoring engine (score.go) has no such gate
// because it is pure.
const benchEnv = "CERBERUS_BENCH"

func main() {
	n := flag.Int("n", 18, "number of infer runs")
	binary := flag.String("binary", "build/cerberus", "path to the cerberus binary")
	healthURL := flag.String("health-url", "http://localhost:8989/health", "target health URL; must return 200")
	workdir := flag.String("workdir", ".", "cwd for the infer call (open-agents repo root)")
	name := flag.String("name", "open-agents", "--name passed to protocol infer")
	from := flag.String("from", "apps/api/src/realtime", "--from passed to protocol infer")
	service := flag.String("service", "api", "--service passed to protocol infer")
	samples := flag.String("samples", "3", "samples hint for the report header (the binary's --samples default)")
	perCall := flag.Duration("per-call-timeout", 120*time.Second, "timeout per infer run")
	flag.Parse()

	if err := runBench(*n, *binary, *healthURL, *workdir, *name, *from, *service, *samples, *perCall); err != nil {
		fmt.Fprintln(os.Stderr, "protoinferbench:", err)
		os.Exit(1)
	}
}

func runBench(n int, binary, healthURL, workdir, name, from, service, samples string, perCall time.Duration) error {
	if os.Getenv(benchEnv) != "1" {
		fmt.Printf("skip (%s unset); set %s=1 to run the benchmark\n", benchEnv, benchEnv)
		return nil
	}
	// LLM creds must be in the environment the binary inherits; fail loud
	// before burning N doomed runs.
	for _, k := range []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL"} {
		if os.Getenv(k) == "" {
			return fmt.Errorf("%s unset; the cerberus binary needs it for LLM calls", k)
		}
	}
	if err := healthCheck(healthURL); err != nil {
		return fmt.Errorf("health check: %w", err)
	}

	results := make([]runResult, 0, n)
	for i := 1; i <= n; i++ {
		stdout, stderr, code, err := runInfer(binary, workdir, name, from, service, perCall)
		if err != nil {
			// Subprocess could not be started or timed out: count as hard_error.
			fmt.Fprintf(os.Stderr, "run %d/%d: exec failed: %v\n", i, n, err)
			results = append(results, runResult{outcome: outcomeHardError})
			continue
		}
		r := classifyRun(stdout, stderr, code)
		if r.outcome != outcomeDraft {
			// One-line diagnostic on the non-draft tail.
			firstLine(stderr, stdout)
			fmt.Fprintf(os.Stderr, "run %d/%d: %s\n", i, n, r.outcome)
		} else {
			fmt.Fprintf(os.Stderr, "run %d/%d: %s\n", i, n, r.outcome)
		}
		results = append(results, r)
	}
	fmt.Println(formatReport(Aggregate(results, samples)))
	return nil
}

func healthCheck(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func runInfer(binary, workdir, name, from, service string, timeout time.Duration) (string, string, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary,
		"protocol", "infer",
		"--name", name,
		"--from", from,
		"--service", service,
		"--dry-run",
	)
	cmd.Dir = workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
		err = nil // non-zero exit is a normal outcome, handled by classifyRun.
	}
	return stdout.String(), stderr.String(), code, err
}

func firstLine(s ...string) {
	for _, t := range s {
		if t == "" {
			continue
		}
		if i := strings.IndexByte(t, '\n'); i >= 0 {
			t = t[:i]
		}
		fmt.Fprintf(os.Stderr, "  stderr/stdout: %s\n", t)
		return
	}
}
```

- [ ] **Step 2: Build and vet**

Run: `go build ./tools/protoinferbench/ && go vet ./tools/protoinferbench/`
Expected: builds clean, no vet warnings.

- [ ] **Step 3: Verify the env-gate skip path (network-free)**

Run: `go run ./tools/protoinferbench -n 2`
Expected: prints `skip (CERBERUS_BENCH unset); set CERBERUS_BENCH=1 to run the benchmark`, exit 0, no network.

- [ ] **Step 4: Verify full `make check` is still green and network-free**

Run: `make check`
Expected: fmt+lint+`go test -race ./...` all PASS, no network/LLM calls (the gate + pure score tests guarantee it).

- [ ] **Step 5: Commit**

```bash
git add tools/protoinferbench/main.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit \
  -m "feat(protoinferbench): env-gated orchestration (health check, N-times exec, report)"
```

---

## Task 3: Real N=18 run, record results, conclusion, push

**Files:**
- Modify: `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` (append `## Repeatable benchmark — 2026-08-02` section)

**Interfaces:**
- Consumes: the built benchmark binary from Task 2.

- [ ] **Step 1: Build cerberus and the benchmark**

Run: `make build && go build -o tools/protoinferbench/protoinferbench ./tools/protoinferbench/`
Expected: `build/cerberus` and `tools/protoinferbench/protoinferbench` exist.

- [ ] **Step 2: Ensure target is up (manual, separate terminal)**

In a separate shell: `cd /home/mason/Documents/code_projects/private/open-agents && fnm use 22 && cd apps/api && npm run dev`. Confirm `curl -sf http://localhost:8989/health` returns ok. (The tool assumes the target is already up; it does not manage the process.)

- [ ] **Step 3: Run the benchmark (N=18)**

From the `open-agents` repo root, with `ANTHROPIC_AUTH_TOKEN` and `ANTHROPIC_BASE_URL` exported in the environment:

```bash
CERBERUS_BENCH=1 \
  /home/mason/Documents/code_projects/private/cerberus/tools/protoinferbench/protoinferbench \
  -n 18 -binary /home/mason/Documents/code_projects/private/cerberus/build/cerberus \
  -workdir /home/mason/Documents/code_projects/private/open-agents
```

Expected: a full markdown report (outcomes + per-structure table + Overall verdict). Capture it verbatim. If the outcome distribution is dominated by `hard_error`/`no_protocol`, inspect the one-line stderr diagnostics before drawing conclusions.

- [ ] **Step 4: Append the report to the dogfood doc**

Append a new section to `cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md` titled `## Repeatable benchmark — 2026-08-02`. Paste the verbatim report table under a `### Results` subsection, plus a short `### Setup` note (N=18, samples=3 per run, env-gated, target up via wrangler dev :8989).

- [ ] **Step 5: Write the Chinese conclusion**

Under `### 结论` in the same new section, write (in Chinese) — adjust to the actual numbers, but cover:
1. 逐结构达标情况（哪些 PASS / 哪些 FAIL）。
2. open-agents 的 protocol infer 当前到底"通过"没有（整体 PASS=7 项全达标）。
3. hard 结构（handshake / batch_items_path）是否值得继续投资——结合失败模式分布判断：若多数运行连 `draft` 都没出，问题在稳定性而非 hard 结构；若 `draft` 占比高但 hard 结构命中率低，则确为识别/转录问题。
4. 建议下一步（转别的区域 or 继续打磨 hard 结构）。

- [ ] **Step 6: Commit and push**

```bash
git add cerberus-docs/technical/dogfood/2026-07-31-m3-3-protocol-infer-dogfood.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com commit \
  -m "docs(dogfood): repeatable protocol-infer benchmark (N=18) results + verdict"
git push origin main
```

---

## Self-Review (run before handoff)

**Spec coverage:**
- Pure scoring + testability (env-gate structural isolation): Task 1 + Task 2. ✓
- 7-structure ground truth + thresholds + PASS=7/7: Task 1 (`Score`, `thresholds`, `Aggregate`). ✓
- Failure runs count in denominator, not dropped: Task 1 `Aggregate` + tests. ✓
- Run-outcome breakdown in report: Task 1 `formatReport`. ✓
- Assume-up + health-check + fail-fast: Task 2 `runBench`/`healthCheck`. ✓
- Real N=18 run + dogfood section + Chinese conclusion + push: Task 3. ✓

**Placeholder scan:** none. testdata fixtures are concrete; `run22-like` is explicitly labeled a reconstruction (honest, not a literal-capture claim).

**Type consistency:** `runResult`/`runOutcome`/`classifyRun`/`Aggregate`/`formatReport` signatures match across Task 1 (definition) and Task 2 (consumption). `Score` returns `[numStructures]bool`; `Aggregate` indexes `h[i]` over `numStructures`. Thresholds and names arrays are both length `numStructures`. `main.go` imports `bytes` (for `bytes.Buffer` in `runInfer`) and `strings` (for `firstLine`); both used. ✓

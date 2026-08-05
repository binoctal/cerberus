# Scout Vocabulary Value Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a repeatable `//go:build manual` test that dumps Scout (ToT) planning output with and without a real WS vocabulary (N=3 each), then classifies every `namespace:action` token in each plan as hit/invented against the real vocabulary — producing evidence of whether vocabulary injection improves planning.

**Architecture:** Three pure helper functions (`vocabTypeSet`, `dumpPlan`, `extractTypeTokens`, `classifyTypes`) live in a non-tagged `*_test.go` file so they compile for both ordinary unit tests and the `manual` integration, but never ship in the production binary. The manual test loads the real `dogfood/ws-realtime` config (auto-loading its vocabulary), builds a fresh `Scout` per condition, and simulates "no vocabulary" by nil-ing `Service.Vocabulary` in memory (byte-equivalent to a missing vocab file — `renderVocabSummary` returns `""`).

**Tech Stack:** Go 1.25, `github.com/binoctal/cerberus`, `internal/head/scout`, `internal/project`, `internal/ai`, `internal/llm`, `modernc.org/sqlite` (`:memory:` store).

## Global Constraints

- Commit author: `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`.
- Code comments and commit messages in English.
- Pure helpers go in `*_test.go` files only — they must not ship in the production binary.
- The manual test must skip cleanly (not fail) when `ANTHROPIC_API_KEY` or `CERBERUS_MODEL` is unset.
- Relative paths from `internal/head/scout/` use `../../../` to reach the repo root (same convention as `setupTestStore`'s `"../../../migrations"`).
- `make test` must stay green and must NOT run the manual test (it is `//go:build manual`).

---

### Task 1: `vocabTypeSet` and `dumpPlan` helpers + unit tests

**Files:**
- Create: `internal/head/scout/vocab_validation_helpers_test.go`
- Create: `internal/head/scout/vocab_validation_helpers_unit_test.go`

**Interfaces:**
- Produces: `func vocabTypeSet(v *project.Vocabulary) map[string]bool` and `func dumpPlan(plan *agent.TestPlan) string`, used by Task 2's classifier and Task 3's manual test.

- [ ] **Step 1: Write the failing tests**

Create `internal/head/scout/vocab_validation_helpers_unit_test.go`:

```go
package scout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestVocabTypeSet(t *testing.T) {
	v := &project.Vocabulary{Edges: []project.VocabEdge{
		{Type: "session:start"},
		{Type: "device:online"},
		{Type: "session:start"}, // duplicate must be collapsed
	}}
	set := vocabTypeSet(v)
	if len(set) != 2 || !set["session:start"] || !set["device:online"] {
		t.Fatalf("vocabTypeSet = %v, want {session:start, device:online}", set)
	}
}

func TestVocabTypeSetNilSafe(t *testing.T) {
	if got := vocabTypeSet(nil); len(got) != 0 {
		t.Fatalf("vocabTypeSet(nil) = %v, want empty", got)
	}
}

func TestDumpPlanContainsReceiveType(t *testing.T) {
	plan := &agent.TestPlan{Goal: "relay", Cases: []agent.TestCase{
		{ID: "tc-1", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "session:created"},
		}},
	}}
	out := dumpPlan(plan)
	if !strings.Contains(out, "session:created") {
		t.Fatalf("dump missing ws_receive type, got:\n%s", out)
	}
	// Must be valid JSON so downstream extraction is stable.
	var check agent.TestPlan
	if err := json.Unmarshal([]byte(out), &check); err != nil {
		t.Fatalf("dump is not valid TestPlan JSON: %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/scout/ -run 'TestVocabTypeSet|TestDumpPlan' -v`
Expected: FAIL / compile error ("undefined: vocabTypeSet", "undefined: dumpPlan").

- [ ] **Step 3: Write the helpers**

Create `internal/head/scout/vocab_validation_helpers_test.go`:

```go
package scout

import (
	"encoding/json"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// vocabTypeSet returns the set of distinct message types declared by a
// vocabulary's edges. Used as the ground-truth type set for classifying
// plan tokens as hit vs invented. Nil-safe (returns empty set).
func vocabTypeSet(v *project.Vocabulary) map[string]bool {
	set := make(map[string]bool)
	if v == nil {
		return set
	}
	for _, e := range v.Edges {
		set[e.Type] = true
	}
	return set
}

// dumpPlan renders a TestPlan as indented JSON so the type-token extractor
// can scan a stable, complete representation (covers ws_send Message payloads
// and ws_receive Type fields alike).
func dumpPlan(plan *agent.TestPlan) string {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		// TestPlan is JSON-safe by construction; fall back to its goal only.
		return plan.Goal
	}
	return string(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run 'TestVocabTypeSet|TestDumpPlan' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/vocab_validation_helpers_test.go internal/head/scout/vocab_validation_helpers_unit_test.go
git commit -m "test(scout): add vocabTypeSet and dumpPlan validation helpers"
```

---

### Task 2: `extractTypeTokens` and `classifyTypes` helpers + unit tests

**Files:**
- Modify: `internal/head/scout/vocab_validation_helpers_test.go` (append two functions)
- Modify: `internal/head/scout/vocab_validation_helpers_unit_test.go` (append tests)

**Interfaces:**
- Consumes: `vocabTypeSet` from Task 1.
- Produces: `func extractTypeTokens(text string) []string` and `func classifyTypes(tokens []string, set map[string]bool) (hits, invented []string)`, used by Task 3.

- [ ] **Step 1: Write the failing tests**

Append to `internal/head/scout/vocab_validation_helpers_unit_test.go`:

```go
func TestExtractTypeTokens(t *testing.T) {
	in := `send {"type":"session:start"} then expect device:online and session:output-batch; session:start again`
	tokens := extractTypeTokens(in)
	got := map[string]bool{}
	for _, tk := range tokens {
		got[tk] = true
	}
	want := map[string]bool{"session:start": true, "device:online": true, "session:output-batch": true}
	for k := range want {
		if !got[k] {
			t.Fatalf("extractTypeTokens missing %q, got %v", k, tokens)
		}
	}
	// Dedup: "session:start" appears twice in the input but once in the slice.
	seen := map[string]int{}
	for _, tk := range tokens {
		seen[tk]++
	}
	for tk, n := range seen {
		if n > 1 {
			t.Fatalf("extractTypeTokens has duplicate %q (%d)", tk, n)
		}
	}
}

func TestClassifyTypes(t *testing.T) {
	set := map[string]bool{"session:start": true, "device:online": true}
	hits, invented := classifyTypes([]string{"session:start", "message:received", "device:online"}, set)
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want 2", hits)
	}
	if len(invented) != 1 || invented[0] != "message:received" {
		t.Fatalf("invented = %v, want [message:received]", invented)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/head/scout/ -run 'TestExtractTypeTokens|TestClassifyTypes' -v`
Expected: FAIL / compile error ("undefined: extractTypeTokens", "undefined: classifyTypes").

- [ ] **Step 3: Write the helpers**

Append to `internal/head/scout/vocab_validation_helpers_test.go`:

```go
import (
	"encoding/json"
	"regexp"
	"sort"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// typeTokenRE matches namespace:action message types like "session:start",
// "device:online", "workflow:task_progress", "session:output-batch". The
// action half allows digits, underscores, and hyphens.
var typeTokenRE = regexp.MustCompile(`[a-z][a-z0-9_]*:[a-z][a-z0-9_-]*`)

// extractTypeTokens returns the distinct namespace:action tokens found in
// text, sorted and de-duplicated. Scanning the full JSON dump captures both
// ws_send payloads and ws_receive type fields.
func extractTypeTokens(text string) []string {
	matches := typeTokenRE.FindAllString(text, -1)
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}

// classifyTypes splits tokens into hits (present in the vocabulary set) and
// invented (absent — model-fabricated types).
func classifyTypes(tokens []string, set map[string]bool) (hits, invented []string) {
	for _, tk := range tokens {
		if set[tk] {
			hits = append(hits, tk)
		} else {
			invented = append(invented, tk)
		}
	}
	return hits, invented
}
```

(Note: merge the `import` block with the existing one — do not add a second `import` declaration.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/head/scout/ -run 'TestExtractTypeTokens|TestClassifyTypes' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Run full scout package tests + fmt**

Run: `make fmt && go test ./internal/head/scout/ -v`
Expected: all existing tests still PASS, new tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/vocab_validation_helpers_test.go internal/head/scout/vocab_validation_helpers_unit_test.go
git commit -m "test(scout): add extractTypeTokens and classifyTypes helpers"
```

---

### Task 3: `//go:build manual` validation test (ToT, N=3 × 2 conditions)

**Files:**
- Modify: `internal/head/scout/vocab_validation_helpers_test.go` (append `cloneConfigWithVocab`)
- Create: `internal/head/scout/vocab_validation_manual_test.go`

**Interfaces:**
- Consumes: `setupTestStore` (existing, `scout_test.go`), `NewScout`, `SetDeepPlan`, `DefaultToTConfig`, `buildModelFromConfig` (Scout method), `project.LoadFromFile`, `llm.NewClient`, `ai.NewDriver`, `ai.NewTokenBudget`, and the Task 1-2 helpers.
- Produces: a skipped-by-default test that, when run with `-tags=manual` plus API key + model env vars, emits plan dumps under `runtime/vocab-validation/` and a per-run hit/invented `t.Logf` summary.

- [ ] **Step 1: Append the config-clone helper**

Append to `internal/head/scout/vocab_validation_helpers_test.go`:

```go
// cloneConfigWithVocab returns a shallow copy of cfg whose services carry the
// given vocabulary (pass nil to simulate a missing vocab file). Used by the
// manual validation test to produce byte-equivalent with/without conditions
// from one loaded config.
func cloneConfigWithVocab(cfg *project.Config, vocab *project.Vocabulary) *project.Config {
	c := *cfg
	svcs := make([]project.Service, len(cfg.Services))
	for i, s := range cfg.Services {
		s.Vocabulary = vocab
		svcs[i] = s
	}
	c.Services = svcs
	return &c
}
```

- [ ] **Step 2: Write the manual test**

Create `internal/head/scout/vocab_validation_manual_test.go`:

```go
//go:build manual

package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// TestVocabValidation_ToT dumps Scout (ToT) planning output for the dogfood
// ws-realtime config, with and without the routing vocabulary, N=3 runs each.
// It classifies every namespace:action token in each dump as hit (in the real
// vocab) or invented, and writes the dumps under runtime/vocab-validation/.
//
// Run manually:
//
//	ANTHROPIC_API_KEY=... CERBERUS_MODEL=claude-sonnet-5 \
//	  go test -tags=manual ./internal/head/scout/ -run TestVocabValidation_ToT -v
//
// The //go:build manual line keeps this file out of the default build and CI
// entirely. Under -tags=manual it still skips (does not fail) when either env
// var is unset, so a manual run without credentials is a clean no-op.
func TestVocabValidation_ToT(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY unset")
	}
	model := os.Getenv("CERBERUS_MODEL")
	if model == "" {
		t.Skip("CERBERUS_MODEL unset")
	}

	cfgPath := filepath.Join("..", "..", "..", "dogfood", "ws-realtime", ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Services, "dogfood config must declare a service")
	require.NotNil(t, cfg.Services[0].Vocabulary, "dogfood config must auto-load a vocabulary")

	realVocab := cfg.Services[0].Vocabulary
	typeSet := vocabTypeSet(realVocab)
	t.Logf("real vocabulary: %d distinct types", len(typeSet))

	const goal = "Cover the realtime WebSocket service's message relay between web and bridge actors: session lifecycle, bridge join/leave signaling, and workflow task progress broadcast. Author WS choreography that drives messages from each role and asserts what each peer receives."

	outDir := filepath.Join("..", "..", "..", "runtime", "vocab-validation")
	require.NoError(t, os.MkdirAll(outDir, 0o755))

	const runsPerCondition = 3
	conditions := []struct {
		name  string
		vocab *project.Vocabulary
	}{
		{"with-vocab", realVocab},
		{"without-vocab", nil},
	}

	for _, cond := range conditions {
		for run := 1; run <= runsPerCondition; run++ {
			label := fmt.Sprintf("%s-run%d", cond.name, run)
			t.Run(label, func(t *testing.T) {
				runCfg := cloneConfigWithVocab(cfg, cond.vocab)
				store := setupTestStore(t)

				client, err := llm.NewClient(model, apiKey)
				require.NoError(t, err)
				driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))

				sct := NewScout(driver, store, runCfg, zap.NewNop())
				sct.SetDeepPlan(DefaultToTConfig(), driver, driver)

				pm := sct.buildModelFromConfig()
				plan, err := sct.Plan(context.Background(), goal, pm)
				require.NoError(t, err)

				dump := dumpPlan(plan)
				require.NoError(t, os.WriteFile(
					filepath.Join(outDir, label+".md"), []byte(dump), 0o644))

				tokens := extractTypeTokens(dump)
				hits, invented := classifyTypes(tokens, typeSet)
				t.Logf("[%s] cases=%d tokens=%d hits=%d invented=%d invented-list=%v",
					label, len(plan.Cases), len(tokens), len(hits), len(invented), invented)
			})
		}
	}
}
```

- [ ] **Step 3: Verify the test is skipped by default (no API call)**

Run: `go test ./internal/head/scout/ -run TestVocabValidation_ToT -v`
Expected: `PASS` with `testing: warning: no tests to run` — the `//go:build manual` file is excluded from the default build, so the test does not exist (and cannot run) under `make test`. (The in-test `t.Skip` is a second layer: under `-tags=manual` without env vars set, it skips rather than fails.)

- [ ] **Step 4: Verify it compiles under the manual tag**

Run: `go vet -tags=manual ./internal/head/scout/`
Expected: no errors (confirms `manual` build compiles even though we did not run the LLM).

- [ ] **Step 5: Run `make check` to confirm nothing else broke**

Run: `make check`
Expected: fmt clean, lint clean, all tests pass.

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/vocab_validation_helpers_test.go internal/head/scout/vocab_validation_manual_test.go
git commit -m "test(scout): add manual vocab validation test (ToT, N=3 x 2 conditions)"
```

---

### Task 4: Run the validation, write the findings report

**Files:**
- Create: `cerberus-docs/technical/validation/2026-08-05-vocab-scout-validation.md`

**Interfaces:**
- Consumes: Task 3's dumps under `runtime/vocab-validation/` and the `t.Logf` hit/invented summary.

This task is the actual experiment — a human+agent step, not TDD.

- [ ] **Step 1: Run the manual test**

Run:
```bash
ANTHROPIC_API_KEY=... CERBERUS_MODEL=claude-sonnet-5 \
  go test -tags=manual ./internal/head/scout/ -run TestVocabValidation_ToT -v 2>&1 | tee runtime/vocab-validation/run.log
```
Expected: 6 sub-tests run (with-vocab ×3, without-vocab ×3); 6 dump files appear under `runtime/vocab-validation/`; each logs a `hits=… invented=… invented-list=…` line.

- [ ] **Step 2: Inspect the dumps and summarize**

Open the 6 dump files. Note per condition:
- The invented type list (commonality across the 3 runs — count a type if it appears in ≥2 of 3).
- Whether choreography follows real routing directions (web→bridge, bridge→web) vs fabricated roles.
- Representative example cases quoted verbatim.

- [ ] **Step 3: Write the report**

Create `cerberus-docs/technical/validation/2026-08-05-vocab-scout-validation.md` with this structure:

```markdown
# Scout Vocabulary Value — Validation Result (2026-08-05)

## Setup
- Config: dogfood/ws-realtime/.cerberus (real open-agents vocab, N types)
- Planner: ToT (default), N=3 runs per condition
- Goal: <paste the goal>

## Type-hit summary
| condition | run | tokens | hits | invented | invented-list |
| --- | --- | --- | --- | --- | --- |
| with-vocab | 1 | … | … | … | … |
...

## Commonality (>=2 of 3 runs)
- With-vocab invented (common): …
- Without-vocab invented (common): …

## Choreography observations
- With-vocab: <routing-direction fidelity, lifecycle coverage>
- Without-vocab: <fabricated types, wrong roles>

## Conclusion
<Meets success criteria? With-vocab invented≈0; without-vocab shows common invented types. State the verdict and any follow-up (e.g. wire vocab into Agent/Examiner, or a negative result requiring prompt rework).>
```

- [ ] **Step 4: Commit the report**

```bash
git add cerberus-docs/technical/validation/2026-08-05-vocab-scout-validation.md
git commit -m "docs(validation): Scout vocabulary value validation result"
```

---

## Optional extension (not part of core validation)

Add a second `t.Run` arm in the manual test that exercises the **direct** planner (drop the `sct.SetDeepPlan(...)` call) under the same two conditions, reusing `dumpPlan` / `extractTypeTokens` / `classifyTypes`. The direct planner also injects vocab (`buildPlanningContext`), so it is worth a glance — but ToT is the production path and the primary subject of this validation. Defer unless the ToT result is inconclusive.

## Risks addressed in-plan

- **Non-determinism:** N=3 + the commonality section in the report separates vocab signal from LLM noise.
- **Type-extraction false positives:** the raw `invented-list` is logged verbatim so a human can discount illustrative-but-unasserted tokens; the signal is the *difference* between conditions, not the absolute count.
- **API-key/model dependency:** the test skips cleanly (Step 3 verifies the skip).
- **Production pollution:** all helpers live in `*_test.go` and never compile into `cmd/cerberus`.

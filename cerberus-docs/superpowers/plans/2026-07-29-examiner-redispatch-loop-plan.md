# Examiner-Driven Targeted Replanning Loop — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close an in-session loop: when the Examiner diagnoses an actionable failure (`RedispatchHint != none`), Scout re-plans a targeted replacement case (`Replaces`), the Agent runs it, the Examiner re-judges — a passed replacement recovers the original target, bounded by a round cap + a no-progress guard.

**Architecture:** Session-orchestrated `executeRepairLoop` between Examiner and Consolidate. Examiner judge tool gains one enum output field; Scout gains `RepairPlan` (new); `TestCase` gains `Replaces`; recovered tally/render (summary + consolidate) gains a `Replaces` branch gated on pass-status. Triple termination: global round cap (default 2) + per-target no-progress (same-hint re-fail) + inherited token-budget backstop (an LLM call that exhausts the budget returns an error → loop breaks).

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), testify, packages `internal/head/agent`, `internal/head/examiner`, `internal/head/scout`, `internal/session`, `internal/config`.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English; answer the user in Simplified Chinese
- Follow existing comment density/naming idiom
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must EXIT 0
- Spec: `cerberus-docs/superpowers/specs/2026-07-29-examiner-redispatch-loop-design.md`
- `RedispatchHint` enum lives in `internal/head/agent` (both examiner and scout already import `agent`; this avoids a scout→examiner cycle)
- Repair is an enhancement, never a run-abort: any error in the loop logs a warning and breaks to Consolidate with whatever verdicts exist

## File Structure

- `internal/head/agent/types.go` — `RedispatchHint` enum + `TestCase.Replaces`
- `internal/head/examiner/types.go` — `JudgeResult.RedispatchHint`
- `internal/head/examiner/policy.go` — `FinalVerdict.RedispatchHint`
- `internal/head/examiner/policy_helpers.go` — propagate hint in `newFinalVerdict`
- `internal/head/examiner/examiner.go` — `fallbackVerdict` → `HintNone`
- `internal/head/examiner/tools.go` — judge_result schema `redispatch_hint`
- `internal/head/examiner/prompts.go` — judge prompt instruction
- `internal/head/examiner/assembly.go` — `assembleJudge` parses hint
- `internal/head/scout/repair_plan.go` (new) — `RepairInput`, `RepairPlan`, `assembleRepair`, `repairTools`, repair prompt consts
- `internal/head/scout/prompts.go` — repair prompt consts (or in repair_plan.go)
- `internal/session/run_phases_repair.go` (new) — `executeRepairLoop`, `buildAgentLoop`, `buildExaminer`
- `internal/session/run_phases_agent.go` — factor loop construction into `buildAgentLoop`
- `internal/session/run_phases_examiner.go` — factor examiner construction into `buildExaminer`
- `internal/session/lifecycle_run.go` — call `executeRepairLoop` after Examiner
- `internal/session/summary.go` — `Replaces` recovered branch
- `internal/session/run_phases_consolidate.go` — `Replaces` skip in shared fallback-skip sites
- `internal/config` (or wherever `Settings` lives) — `ReplanMaxRounds` + resolver

---

### Task 1: Data model — `RedispatchHint` enum, `TestCase.Replaces`, verdict fields

**Files:**
- Modify: `internal/head/agent/types.go`
- Modify: `internal/head/examiner/types.go`, `policy.go`, `policy_helpers.go`, `examiner.go`
- Test: `internal/head/examiner/policy_redispatch_test.go` (new)

**Interfaces:**
- Produces: `agent.RedispatchHint` (`HintNone`/`HintEndpointDrift`/`HintAuth`/`HintShape`); `agent.TestCase.Replaces string`; `examiner.JudgeResult.RedispatchHint agent.RedispatchHint`; `examiner.FinalVerdict.RedispatchHint agent.RedispatchHint`; `newFinalVerdict` propagates it; `fallbackVerdict` sets `HintNone`. Consumed by Tasks 2–6.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/examiner/policy_redispatch_test.go
package examiner

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// TestNewFinalVerdict_PropagatesRedispatchHint: the hint parsed by the judge
// flows onto the FinalVerdict so the repair loop (Task 4) can read it.
func TestNewFinalVerdict_PropagatesRedispatchHint(t *testing.T) {
	for _, hint := range []agent.RedispatchHint{
		agent.HintNone, agent.HintEndpointDrift, agent.HintAuth, agent.HintShape,
	} {
		jr := &JudgeResult{Status: StatusFail, RedispatchHint: hint}
		v := newFinalVerdict(jr, agent.StepResult{})
		if v.RedispatchHint != hint {
			t.Fatalf("hint %q not propagated to FinalVerdict (got %q)", hint, v.RedispatchHint)
		}
	}
}

// TestFallbackVerdict_HintNone: a degraded/fallback verdict must not accidentally
// trigger replanning — it defaults to HintNone.
func TestFallbackVerdict_HintNone(t *testing.T) {
	v := fallbackVerdict(agent.StepResult{Status: agent.StepFailed}, 0.5, "examiner unavailable")
	if v.RedispatchHint != agent.HintNone {
		t.Fatalf("fallback verdict must default to HintNone, got %q", v.RedispatchHint)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/examiner/ -run 'TestNewFinalVerdict_PropagatesRedispatchHint|TestFallbackVerdict_HintNone' -v`
Expected: COMPILE ERROR — `agent.HintNone` undefined; `JudgeResult.RedispatchHint` / `FinalVerdict.RedispatchHint` unknown fields.

- [ ] **Step 3: Add the enum + Replaces field (agent)**

In `internal/head/agent/types.go`, add the enum near the top (after the `StepStatus` consts or beside `TestCase`):

```go
// RedispatchHint is the Examiner's structured diagnosis of a failure's
// correctable cause (feature #3). "none" means no targeted replanning; the
// others name a cause a replacement case could address. Defined in package
// agent (not examiner) so both scout and examiner can reference it without a
// scout->examiner import cycle.
type RedispatchHint string

const (
	HintNone          RedispatchHint = "none"
	HintEndpointDrift RedispatchHint = "endpoint_drift" // wrong path/method/verb
	HintAuth          RedispatchHint = "auth"           // missing/bad credentials or scheme
	HintShape         RedispatchHint = "shape"          // wrong payload/contract shape
)
```

Add the `Replaces` field to `TestCase` (after `FallbackFor`):

```go
	// Replaces is the ID of the failed case this case is a targeted replacement
	// for (feature #3). Empty on normal/planned cases. A replacement is scheduled
	// explicitly by the repair loop (NOT lazily activated like FallbackFor).
	Replaces string `json:"replaces,omitempty"`
```

- [ ] **Step 4: Add hint to JudgeResult + FinalVerdict + propagate (examiner)**

In `internal/head/examiner/types.go`, add to `JudgeResult`:
```go
	RedispatchHint agent.RedispatchHint `json:"redispatch_hint,omitempty"`
```

In `internal/head/examiner/policy.go`, add to `FinalVerdict`:
```go
	RedispatchHint agent.RedispatchHint
```

In `internal/head/examiner/policy_helpers.go`, add to the `FinalVerdict{...}` literal in `newFinalVerdict`:
```go
		RedispatchHint:        judgeResult.RedispatchHint,
```

In `internal/head/examiner/examiner.go`, in `fallbackVerdict`, the returned `FinalVerdict` already defaults `RedispatchHint` to the zero value (`""`). Make it explicit so the intent is clear and resilient to future field reordering — set the field:
```go
	return FinalVerdict{
		Status:                stepStatusToJudgeStatus(r.Status),
		RedispatchHint:        agent.HintNone,
		Reasoning:             reason,
		ExistenceConfidence:   conf,
		StepResult:            r,
	}
```
(Read the current `fallbackVerdict` literal first and only add the `RedispatchHint: agent.HintNone,` line if the other fields already match — do not drop existing fields.)

- [ ] **Step 5: Run test to verify GREEN**

Run: `go test ./internal/head/examiner/ -run 'TestNewFinalVerdict_PropagatesRedispatchHint|TestFallbackVerdict_HintNone' -v`
Expected: PASS.

- [ ] **Step 6: Regression — build + agent/examiner packages**

Run: `go build ./... && go test ./internal/head/agent/ ./internal/head/examiner/ -count=1`
Expected: PASS. `Replaces` is unused so far (consumed later); `go vet`/lint must not flag an unused struct field (Go does not).

- [ ] **Step 7: Commit**

```bash
git add internal/head/agent/types.go internal/head/examiner/types.go internal/head/examiner/policy.go internal/head/examiner/policy_helpers.go internal/head/examiner/examiner.go internal/head/examiner/policy_redispatch_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): RedispatchHint enum + TestCase.Replaces + verdict hint fields

agent.RedispatchHint (none/endpoint_drift/auth/shape) defined in package agent
so scout and examiner both reference it without a cycle. TestCase gains Replaces
(runtime targeted-patch link, distinct from FallbackFor). JudgeResult and
FinalVerdict carry the hint; newFinalVerdict propagates it; fallbackVerdict
defaults to HintNone so a degraded Examiner never triggers replanning."
```

---

### Task 2: Examiner judge tool emits `redispatch_hint`

**Files:**
- Modify: `internal/head/examiner/tools.go` (`judgeTools` → `judge_result` schema)
- Modify: `internal/head/examiner/assembly.go` (`assembleJudge`)
- Modify: `internal/head/examiner/prompts.go` (judge prompt)
- Test: `internal/head/examiner/assembly_test.go` (extend) or `assembly_redispatch_test.go` (new)

**Interfaces:**
- Consumes: `agent.RedispatchHint` (Task 1).
- Produces: `assembleJudge` populates `JudgeResult.RedispatchHint` from the tool call's `redispatch_hint` field (4 categories; missing/bogus → `HintNone`).

- [ ] **Step 1: Write the failing test**

```go
// internal/head/examiner/assembly_redispatch_test.go
package examiner

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// TestAssembleJudge_ParsesRedispatchHint: the judge_result tool's
// redispatch_hint enum maps to the agent.RedispatchHint constants; a missing or
// unrecognized value collapses to HintNone (never triggers replanning by
// accident).
func TestAssembleJudge_ParsesRedispatchHint(t *testing.T) {
	cases := map[string]agent.RedispatchHint{
		"endpoint_drift": agent.HintEndpointDrift,
		"auth":           agent.HintAuth,
		"shape":          agent.HintShape,
		"none":           agent.HintNone,
		"":               agent.HintNone,
		"bogus":          agent.HintNone,
	}
	for in, want := range cases {
		input := map[string]any{"status": "fail", "reasoning": "r"}
		if in != "" {
			input["redispatch_hint"] = in
		}
		jr, err := assembleJudge(llm.ToolCall{Name: "judge_result", Input: input})
		if err != nil {
			t.Fatalf("assembleJudge error for %q: %v", in, err)
		}
		if jr.RedispatchHint != want {
			t.Fatalf("input %q: want hint %q, got %q", in, want, jr.RedispatchHint)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestAssembleJudge_ParsesRedispatchHint -v`
Expected: FAIL — `jr.RedispatchHint` is always `""` (assembleJudge does not read the field).

- [ ] **Step 3: Add the field to the tool schema + parse it**

In `internal/head/examiner/tools.go`, the `judge_result` tool's `InputSchema` is built by `llm.ObjSchema(required []any, props map[string]any)`. Add `redispatch_hint` to the props map (NOT to the required list — a missing value defaults to none):

```go
		InputSchema: llm.ObjSchema([]any{"status", "existence_confidence", "correctness_confidence", "reasoning"}, map[string]any{
			"status":                 map[string]any{"type": "string", "enum": []any{"pass", "fail", "skip", "uncertain"}},
			"existence_confidence":   map[string]any{"type": "number"},
			"correctness_confidence": map[string]any{"type": "number"},
			"reasoning":              map[string]any{"type": "string"},
			"redispatch_hint": map[string]any{
				"type": "string",
				"enum": []any{"none", "endpoint_drift", "auth", "shape"},
				"description": "For a fail: the correctable root cause a replacement case could address. 'none' unless the failure is clearly correctable.",
			},
		}),
```
(Keep all existing fields/properties; only add the `redispatch_hint` entry. Read the current literal first.)

In `internal/head/examiner/assembly.go`, add parsing to `assembleJudge`'s returned `JudgeResult{...}`:
```go
		RedispatchHint: parseRedispatchHint(llm.StrField(call, "redispatch_hint")),
```
and add the helper near `assembleJudge`:
```go
// parseRedispatchHint maps the LLM's redispatch_hint string to the enum; any
// missing or unrecognized value becomes HintNone so a malformed/omitted hint
// never accidentally triggers replanning.
func parseRedispatchHint(s string) agent.RedispatchHint {
	switch agent.RedispatchHint(s) {
	case agent.HintEndpointDrift, agent.HintAuth, agent.HintShape:
		return agent.RedispatchHint(s)
	default:
		return agent.HintNone
	}
}
```

- [ ] **Step 4: Update the judge prompt**

In `internal/head/examiner/prompts.go`, find the judge prompt (the const that instructs emitting `judge_result`). Append one instruction line describing `redispatch_hint`, e.g.:

```
For a 'fail' verdict, also set redispatch_hint to the correctable root cause if any: 'endpoint_drift' (wrong path/method/verb), 'auth' (missing/bad credentials or scheme), or 'shape' (wrong payload/contract). Use 'none' for pass/skip or any non-correctable failure. Put the diagnostic detail in reasoning, not in a separate field.
```
(Read the actual prompt const first; append the instruction without removing existing guidance.)

- [ ] **Step 5: Run test to verify GREEN**

Run: `go test ./internal/head/examiner/ -run TestAssembleJudge_ParsesRedispatchHint -v`
Expected: PASS.

- [ ] **Step 6: Regression — examiner package**

Run: `go test ./internal/head/examiner/ -count=1`
Expected: PASS. Existing judge tests unaffected (the new field is optional).

- [ ] **Step 7: Commit**

```bash
git add internal/head/examiner/tools.go internal/head/examiner/assembly.go internal/head/examiner/prompts.go internal/head/examiner/assembly_redispatch_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): examiner judge tool emits redispatch_hint

judge_result schema gains an optional redispatch_hint enum
(none/endpoint_drift/auth/shape); assembleJudge parses it (missing/bogus -> none);
the judge prompt instructs emitting it only for correctable failures."
```

---

### Task 3: Scout `RepairPlan` + `assembleRepair`

**Files:**
- Create: `internal/head/scout/repair_plan.go`
- Test: `internal/head/scout/repair_plan_test.go` (new)

**Interfaces:**
- Consumes: `agent.TestCase`, `agent.RedispatchHint` (Task 1), `s.driver.DecideWithTools`, `llm.StrField`.
- Produces: `RepairInput{Case agent.TestCase, Hint agent.RedispatchHint, Reasoning string}`; `RepairPlan(ctx, goal, model, []RepairInput) ([]agent.TestCase, error)`; `assembleRepair(calls, failures) []agent.TestCase` — one replacement per failure, `Replaces` paired, unpaired emissions/failures dropped.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/scout/repair_plan_test.go
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssembleRepair_PairsReplacements: each repair_case emission pairs to its
// originating failure via Replaces (one-to-one, input order); an emission whose
// `replaces` matches no failure is dropped; a failure with no emission yields no
// replacement.
func TestAssembleRepair_PairsReplacements(t *testing.T) {
	failures := []RepairInput{
		{Case: agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}, Hint: agent.HintEndpointDrift, Reasoning: "404"},
		{Case: agent.TestCase{ID: "tc-2", Target: "/login", Method: "POST", Service: "api"}, Hint: agent.HintAuth, Reasoning: "401"},
	}
	calls := []llm.ToolCall{
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-1", "method": "GET", "path": "/v2/users", "service": "api", "expectation": "200"}},
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-2", "method": "POST", "path": "/login", "service": "api", "body": "{\"x\":1}", "expectation": "200"}},
		// Unmatched: replaces a non-existent failure -> dropped.
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-9", "method": "GET", "path": "/x", "service": "api"}},
		// Duplicate for tc-1 -> dropped (one replacement per failure).
		{Name: "repair_case", Input: map[string]any{"replaces": "tc-1", "method": "GET", "path": "/v3/users", "service": "api"}},
	}

	out := assembleRepair(calls, failures)
	require.Len(t, out, 2, "one replacement per matched failure")
	assert.Equal(t, "tc-1", out[0].Replaces)
	assert.Equal(t, "/v2/users", out[0].Target)
	assert.Equal(t, "GET", out[0].Method)
	assert.Equal(t, "tc-2", out[1].Replaces)
	assert.NotEmpty(t, out[1].Body, "body carried through")

	// A failure with no matching emission produces nothing.
	out2 := assembleRepair(nil, failures[:1])
	assert.Empty(t, out2)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout/ -run TestAssembleRepair_PairsReplacements -v`
Expected: COMPILE ERROR — `RepairInput` and `assembleRepair` undefined.

- [ ] **Step 3: Implement `RepairPlan`, `assembleRepair`, `RepairInput`, `repairTools`**

```go
// internal/head/scout/repair_plan.go
package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// RepairInput is one Examiner-diagnosed failure handed to Scout for targeted
// re-planning (feature #3). Hint+Reasoning guide the corrected case; Case is the
// original (its ID becomes the replacement's Replaces).
type RepairInput struct {
	Case      agent.TestCase
	Hint      agent.RedispatchHint
	Reasoning string
}

// RepairPlan asks the LLM for one corrected case per failure (feature #3). Each
// emitted repair_case tool call becomes a TestCase with Replaces set to its
// originating failure's ID. Degrades on LLM error: returns (nil, err) so the
// repair loop can log+break without aborting the run.
func (s *Scout) RepairPlan(ctx context.Context, goal string, model *project.ProjectModel, failures []RepairInput) ([]agent.TestCase, error) {
	if len(failures) == 0 {
		return nil, nil
	}
	prompt := s.buildRepairPrompt(goal, model, failures)
	res, err := s.driver.DecideWithTools(ctx, prompt, repairTools())
	if err != nil {
		return nil, fmt.Errorf("repair plan: %w", err)
	}
	return assembleRepair(res.ToolCalls, failures), nil
}

// assembleRepair maps repair_case tool calls to replacement TestCases, pairing
// each to its originating failure via Replaces. One replacement per failure
// (first emission wins); an emission whose `replaces` matches no failure is
// dropped, as is any failure with no matching emission. Iterates failures in
// input order for deterministic output.
func assembleRepair(calls []llm.ToolCall, failures []RepairInput) []agent.TestCase {
	byID := make(map[string]int, len(failures))
	for i, f := range failures {
		byID[f.Case.ID] = i
	}
	used := make(map[int]bool, len(failures))
	var out []agent.TestCase
	for _, call := range calls {
		if call.Name != "repair_case" {
			continue
		}
		id := llm.StrField(call, "replaces")
		idx, ok := byID[id]
		if !ok || used[idx] {
			continue
		}
		used[idx] = true
		out = append(out, repairCaseFromCall(call, id))
	}
	return out
}

// repairCaseFromCall builds the corrected TestCase from a repair_case emission.
// Target/Service/Method/Body carry the correction; Replaces binds it to the
// failed case. A deterministic ID makes traces/reporting readable.
func repairCaseFromCall(call llm.ToolCall, replaces string) agent.TestCase {
	return agent.TestCase{
		ID:          fmt.Sprintf("repair-%s", replaces),
		Name:        fmt.Sprintf("repair %s", llm.StrField(call, "path")),
		Target:      llm.StrField(call, "path"),
		Method:      llm.StrField(call, "method"),
		Service:     llm.StrField(call, "service"),
		Body:        llm.StrField(call, "body"),
		Expectation: llm.StrField(call, "expectation"),
		Replaces:    replaces,
	}
}
```

Add `repairTools()` in `repair_plan.go` (mirror `planTools()` shape in `tools.go`; one tool, one call per failure):

```go
// repairTools returns the tool surface for RepairPlan: one repair_case call per
// failed case, carrying the corrected fields plus `replaces` to bind it back.
func repairTools() []llm.Tool {
	return []llm.Tool{
		{
			Name:        "repair_case",
			Description: "Emit a corrected test case that replaces one failed case. One call per failed case.",
			InputSchema: llm.ObjSchema([]any{"replaces", "method", "path"}, map[string]any{
				"replaces":     map[string]any{"type": "string", "description": "ID of the failed case this replaces"},
				"method":       map[string]any{"type": "string"},
				"path":         map[string]any{"type": "string"},
				"service":      map[string]any{"type": "string"},
				"body":         map[string]any{"type": "string"},
				"expectation":  map[string]any{"type": "string"},
			}),
		},
	}
}
```

Add `buildRepairPrompt` in `repair_plan.go` (mirror `runAIPlanning`'s `ai.NewPrompt()` usage):

```go
func (s *Scout) buildRepairPrompt(goal string, model *project.ProjectModel, failures []RepairInput) string {
	var b []byte
	b = append(b, fmt.Sprintf("Goal: %s\n\nYou are repairing failed test cases. For EACH failed case below, emit ONE repair_case tool call with the corrected fields (set `replaces` to the failed case's ID). Only change what the diagnosis indicates; keep the rest.\n\n", goal)...)
	for i, f := range failures {
		b = append(b, fmt.Sprintf("## Failure %d (replaces=%s)\n- target: %s %s (service=%s)\n- body: %q\n- expectation: %s\n- diagnosis hint: %s\n- reasoning: %s\n\n",
			i+1, f.Case.ID, f.Case.Method, f.Case.Target, f.Case.Service,
			f.Case.Body, f.Case.Expectation, f.Hint, f.Reasoning)...)
	}
	return ai.NewPrompt().
		System(promptRepairSystem).
		Context(string(b)).
		Task("Emit one repair_case tool call per failed case above.").
		Output(promptRepairToolGuide).
		Build()
}
```

Add the prompt consts (in `repair_plan.go` or `prompts.go`):

```go
const promptRepairSystem = `You are a test-repair agent. Given failed cases with an Examiner diagnosis, emit exactly one corrected test case per failure via the repair_case tool. Correct only what the diagnosis indicates (wrong path/method = endpoint_drift; credentials = auth; payload = shape). Set ` + "`replaces`" + ` to the failed case's ID.`

const promptRepairToolGuide = `Emit ONE repair_case TOOL CALL PER FAILED CASE. Do not output JSON.`
```

- [ ] **Step 4: Run test to verify GREEN**

Run: `go test ./internal/head/scout/ -run TestAssembleRepair_PairsReplacements -v`
Expected: PASS.

- [ ] **Step 5: Regression — scout package**

Run: `go test ./internal/head/scout/ -count=1`
Expected: PASS. (`RepairPlan`/`buildRepairPrompt` are not yet called from production code; they compile and the assembler is covered.)

- [ ] **Step 6: Commit**

```bash
git add internal/head/scout/repair_plan.go internal/head/scout/repair_plan_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): scout RepairPlan emits targeted replacement cases

RepairPlan asks the LLM for one corrected case per Examiner-diagnosed failure;
assembleRepair pairs each repair_case emission to its failure via Replaces
(one-to-one, input order, unpaired/duplicate emissions dropped). repairTools
surfaces the repair_case tool. Not wired into the run yet (Task 4)."
```

---

### Task 4: `executeRepairLoop` (one round) + shared builders + wire into Run

**Files:**
- Create: `internal/session/run_phases_repair.go`
- Modify: `internal/session/run_phases_agent.go` (factor `buildAgentLoop`)
- Modify: `internal/session/run_phases_examiner.go` (factor `buildExaminer`)
- Modify: `internal/session/lifecycle_run.go` (call loop after Examiner)
- Modify: the `Settings` struct (add `ReplanMaxRounds`) + a resolver
- Test: `internal/session/repair_loop_test.go` (new)

**Interfaces:**
- Consumes: `scout.RepairInput`, `scout.Scout.RepairPlan` (Task 3); `agent.RedispatchHint`, `agent.TestCase.Replaces` (Task 1); `examiner.FinalVerdict.RedispatchHint` (Tasks 1–2).
- Produces: `executeRepairLoop(model)` runs rounds; `buildAgentLoop()` / `buildExaminer()` shared helpers. This task implements ONE round + the round cap; no-progress + budget backstop are Task 5.

**Prerequisite refactor detail:** `executeAgentPhase` (`run_phases_agent.go:13`) builds the loop inline (lines 24–38) then runs it (47–56). Move the construction (engine, multiExec, config, emb, loop literal) into `rp.buildAgentLoop() *agent.ReActLoop` (return the loop; for the parallel path also expose a builder or build the loop and wrap on demand). `executeAgentPhase` calls `buildAgentLoop()` then runs. `executeExaminerPhase` (`run_phases_examiner.go:12`) builds the examiner inline (14–21); move into `rp.buildExaminer() *examiner.Examiner`.

- [ ] **Step 1: Write the failing test**

```go
// internal/session/repair_loop_test.go
package session_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/stretchr/testify/require"
)

// TestExecuteRepairLoop_OneRound: given verdicts with one actionable failure
// (HintEndpointDrift) and a stub Scout/Agent/Examiner, the loop runs exactly one
// repair round: it requests a replacement, runs it, re-judges, and merges the
// replacement verdict. (Uses the existing session test harness; if a stubbed
// Scout is hard to inject, assert on the observable effect — a replacement case
// with Replaces set appears in the persisted plan and a replacement verdict is
// persisted. See implementer note.)
func TestExecuteRepairLoop_OneRound(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t) // existing helper in internal/session/*_test.go
	defer cleanup()

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, RedispatchHint: agent.HintEndpointDrift,
			StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}}},
	}
	// ...wire rp.plan/model + a Scout whose RepairPlan returns one replacement
	// (Replaces="tc-1") + an executor+examiner that pass it...
	require.NoError(t, rp.executeRepairLoop(rp.model))

	// A replacement verdict is present and merged.
	require.NotEmpty(t, rp.verdicts)
	var sawReplacement bool
	for _, v := range rp.verdicts {
		if v.StepResult.TestCase != nil && v.StepResult.TestCase.Replaces == "tc-1" {
			sawReplacement = true
		}
	}
	require.True(t, sawReplacement, "replacement verdict must be merged into rp.verdicts")
}
```

> **Implementer note:** the loop builds Scout/Agent/Examiner from `rp.session`. If the session test harness cannot inject a stub Scout returning a deterministic replacement, write the test at the level that IS observable: seed `rp.verdicts` with an actionable failure, call `executeRepairLoop`, and assert (a) at least one replacement case (`Replaces != ""`) was appended to the persisted plan (`Store.GetPlan`) and (b) `PersistFinalVerdicts` wrote a verdict for a `Replaces != ""` case. If full DI is impractical in the harness, add a thin seam: `rp.repairPlanFn func(...) ([]agent.TestCase, error)` defaulting to `scoutHead.RepairPlan`, overridable in tests. Both forms prove the same one-round contract; pick the one that compiles against the real harness.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestExecuteRepairLoop_OneRound -v`
Expected: FAIL — `executeRepairLoop` undefined.

- [ ] **Step 3: Factor shared builders**

In `run_phases_agent.go`, extract the construction into:
```go
// buildAgentLoop constructs the Agent execution loop from session config. Shared
// by executeAgentPhase (full plan) and the repair loop (replacement subset).
func (rp *runPhase) buildAgentLoop() *agent.ReActLoop {
	projectDir := rp.session.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}
	engine := agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), agent.BuildWSProtocolIndex(rp.session.Config), rp.session.Gate, rp.session.Logger)
	emb := embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	return agent.NewReActLoopWithGateWithConfig(agent.ReActLoopConfig{
		Driver:   rp.session.driverFor(&rp.session.agentDriver),
		Store:    rp.session.Store,
		Engine:   engine,
		Executor: multiExec,
		Config:   agent.DefaultReActConfig(),
		Gate:     rp.session.Gate,
		Logger:   rp.session.Logger,
		Embedder: emb,
		Project:  rp.session.Config.Project.Name,
	})
}
```
Rewrite `executeAgentPhase` to call `rp.buildAgentLoop()` and then run (keep the existing parallel/sequential branch unchanged — it uses `loop`/`pExec`). If the parallel path needs the loop too, build via `buildAgentLoop()` and wrap with `NewParallelExecutor` as today.

In `run_phases_examiner.go`, extract:
```go
// buildExaminer constructs the Examiner head from session config. Shared by
// executeExaminerPhase and the repair loop (re-judge of replacements).
func (rp *runPhase) buildExaminer() *examiner.Examiner {
	examinerCfg := examiner.DefaultExaminerConfig()
	if rp.session.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = rp.session.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = rp.session.Config.Settings.AutoFix
	}
	examinerCfg.MaxWorkers = rp.session.MaxWorkers
	return examiner.NewExaminer(rp.session.driverFor(&rp.session.examinerDriver), rp.session.criticDriver, rp.session.Store, examinerCfg, rp.session.Logger)
}
```
Rewrite `executeExaminerPhase` to call `rp.buildExaminer()` then `Examine`.

- [ ] **Step 4: Add the round-cap setting**

Find the `Settings` struct (grep for `ConfidenceThreshold` — the same struct that holds it; likely in `internal/project` or `internal/config`). Add:
```go
	// ReplanMaxRounds caps the in-session Examiner->Scout repair loop (feature #3).
	// 0 keeps the default. <=0 absent means "use default" resolved by the resolver.
	ReplanMaxRounds int `json:"replan_max_rounds,omitempty" yaml:"replan_max_rounds,omitempty"`
```
Add a resolver next to the other `Resolve*` helpers (e.g. `config.ResolveReplanMaxRounds`):
```go
// ResolveReplanMaxRounds returns the repair-loop round cap, defaulting to 2.
func ResolveReplanMaxRounds(s Settings) int {
	if s.ReplanMaxRounds > 0 {
		return s.ReplanMaxRounds
	}
	return 2
}
```
(Put the resolver in the same package as the existing `ResolveReflexionConfig`/`ResolveToTConfig`; mirror their signature — they take `rp.session.Config.Settings`.)

- [ ] **Step 5: Implement `executeRepairLoop` (one round + cap)**

```go
// internal/session/run_phases_repair.go
package session

import (
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"go.uber.org/zap"
)

// executeRepairLoop closes the in-session Examiner->Scout loop (feature #3):
// each round, collect failures with an actionable RedispatchHint, ask Scout for
// targeted replacements (Replaces), run only those, and re-judge. Bounded by a
// round cap (default 2) and the no-progress guard (Task 5). Any error logs and
// breaks to Consolidate — repair never aborts the run.
func (rp *runPhase) executeRepairLoop(model interface{}) error {
	maxRounds := resolveReplanMaxRounds(rp.session)
	stuck := map[string]bool{} // Task 5 populates this; empty here = no no-progress yet
	scoutHead := scout.NewScout(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Store, rp.session.Config, rp.session.Logger)

	for round := 1; round <= maxRounds; round++ {
		eligible := rp.eligibleFailures(stuck)
		if len(eligible) == 0 {
			break
		}
		rp.session.Logger.Info("repair round", zap.Int("round", round), zap.Int("eligible", len(eligible)))

		replacements, err := scoutHead.RepairPlan(rp.ctx, rp.session.Goal, nil, eligible)
		if err != nil {
			rp.session.Logger.Warn("repair plan failed; stopping repair loop", zap.Error(err))
			return nil
		}
		if len(replacements) == 0 {
			rp.session.Logger.Info("repair plan produced no replacements; stopping")
			return nil
		}

		// Append + persist so resume sees them.
		rp.plan.Cases = append(rp.plan.Cases, replacements...)
		if err := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, rp.plan); err != nil {
			rp.session.Logger.Warn("save plan (repair) failed", zap.Error(err))
		}

		// Execute only the replacements.
		loop := rp.buildAgentLoop()
		subPlan := &agent.TestPlan{Goal: rp.plan.Goal, Cases: replacements, ProjectURL: rp.plan.ProjectURL}
		repResults, err := loop.ExecutePlan(rp.ctx, subPlan, rp.session.ID)
		if err != nil {
			rp.session.Logger.Warn("repair execute failed; stopping repair loop", zap.Error(err))
			return nil
		}

		// Re-judge only the replacement results; merge.
		repVerdicts, _, err := rp.buildExaminer().Examine(rp.ctx, repResults, rp.session.ID, rp.session.Config.Project.Name)
		if err != nil {
			rp.session.Logger.Warn("repair re-judge failed; stopping repair loop", zap.Error(err))
			return nil
		}
		rp.results = append(rp.results, repResults...)
		rp.verdicts = append(rp.verdicts, repVerdicts...)
		if _, err := examiner.PersistFinalVerdicts(rp.ctx, rp.session.Store, rp.session.Logger, rp.session.ID, repVerdicts); err != nil {
			rp.session.Logger.Warn("persist repair verdicts failed", zap.Error(err))
		}
	}
	return nil
}

// eligibleFailures collects Fail verdicts with an actionable hint whose target is
// not marked stuck. Returns RepairInput for Scout.
func (rp *runPhase) eligibleFailures(stuck map[string]bool) []scout.RepairInput {
	var out []scout.RepairInput
	for _, v := range rp.verdicts {
		if v.Status != examiner.StatusFail || v.RedispatchHint == agent.HintNone {
			continue
		}
		tc := v.StepResult.TestCase
		if tc == nil {
			continue
		}
		if stuck[normalizeTargetForStuck(tc.Target)] {
			continue
		}
		out = append(out, scout.RepairInput{Case: *tc, Hint: v.RedispatchHint, Reasoning: v.Reasoning})
	}
	return out
}

// normalizeTargetForStuck keys the stuck map. Reuse memory.NormalizeTarget if
// available in this package (consolidate already imports it); else lowercase +
// trim. Keeping it aligned with consolidate's normalization avoids split keys.
func normalizeTargetForStuck(target string) string {
	// Prefer memory.NormalizeTarget (same key consolidate uses). If the import
	// is awkward here, a lowercase+trim fallback is acceptable — it only needs
	// within-loop stability.
	return normalizedStuckKey(target)
}
```

> **Implementer notes:**
> - `resolveReplanMaxRounds(rp.session)` calls the resolver from Step 4 (pass `rp.session.Config.Settings`). Adjust the call to the resolver's real package/signature.
> - `model` param: `executeScoutPhase` returns `*project.ProjectModel`; the lifecycle caller has it. If `RepairPlan` doesn't need the model (the repair prompt derives corrections from the failed case + hint, not the model), drop the param. Prefer dropping it — update `RepairPlan`'s signature in Task 3 if so (remove `model`). Keep the signature consistent across tasks.
> - `normalizedStuckKey`: implement as `memory.NormalizeTarget(target)` (import `internal/memory` — consolidate already does). Define `func normalizedStuckKey(t string) string { return memory.NormalizeTarget(t) }` or call `memory.NormalizeTarget` directly at the call site and delete the wrapper. Pick one; do not leave both.
> - If a test seam (`repairPlanFn`) was added per Step 1's note, call it instead of `scoutHead.RepairPlan` when non-nil.

- [ ] **Step 6: Wire into Run**

In `internal/session/lifecycle_run.go`, after `executeExaminerPhase()` and before `executeConsolidatePhase()`:
```go
	// Phase 3.1: Repair loop — Examiner->Scout targeted replanning (feature #3).
	if err := rp.executeRepairLoop(model); err != nil {
		rp.session.Logger.Warn("repair loop failed", zap.Error(err))
	}
```

- [ ] **Step 7: Run test to verify GREEN; fix compile**

Run: `go build ./... && go test ./internal/session/ -run TestExecuteRepairLoop_OneRound -v`
Expected: PASS. Fix any signature mismatches (e.g. `RepairPlan` model param, resolver package).

- [ ] **Step 8: Regression — session package**

Run: `go test ./internal/session/ -count=1`
Expected: PASS. Existing runs unaffected when no verdict has an actionable hint (the loop's first `eligibleFailures` returns empty and it returns immediately).

- [ ] **Step 9: Commit**

```bash
git add internal/session/run_phases_repair.go internal/session/run_phases_agent.go internal/session/run_phases_examiner.go internal/session/lifecycle_run.go internal/session/repair_loop_test.go <settings file>
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): executeRepairLoop — one-round Examiner->Scout repair

Session-orchestrated loop between Examiner and Consolidate: collects actionable
failures, Scout.RepairPlan emits Replaces-bound replacements, Agent runs only
those, Examiner re-judges and merges. buildAgentLoop/buildExaminer factored out
of their phase funcs for reuse. Round cap (default 2, Settings.replan_max_rounds).
No-progress + budget backstop land in Task 5."
```

---

### Task 5: Termination — no-progress guard + budget backstop

**Files:**
- Modify: `internal/session/run_phases_repair.go` (stuck-map population)
- Test: `internal/session/repair_loop_noprogress_test.go` (new)

**Interfaces:**
- Consumes: `FinalVerdict.RedispatchHint`, `TestCase.Replaces` (the chain).
- Produces: a target is `stuck` (dropped from later rounds) when its replacement re-fails with the SAME hint category as its predecessor (walk `Replaces` to the prior verdict). The budget backstop is inherited: an LLM call that exhausts the token budget returns an error → `RepairPlan` returns err → the loop breaks (Task 4 already handles it).

- [ ] **Step 1: Write the failing test**

```go
// internal/session/repair_loop_noprogress_test.go
package session_test

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/stretchr/testify/assert"
)

// TestNoProgress_SameHintRefailDropsTarget: a replacement that re-fails with the
// SAME hint as its predecessor marks the target stuck — it must NOT be sent to
// Scout again. A hint CHANGE (drift->auth) is progress and stays eligible.
func TestNoProgress_SameHintRefailDropsTarget(t *testing.T) {
	// Build two target histories in rp.verdicts:
	//  target /users: original fail(drift) + replacement fail(drift)  -> stuck
	//  target /login: original fail(drift) + replacement fail(auth)   -> eligible
	// Assert eligibleFailures(stuck) excludes /users and includes /login.
	_ = examiner.StatusFail
	_ = agent.HintEndpointDrift
	// (Construct rp via newTestRunPhase; populate verdicts; call the stuck
	// computation + eligibleFailures; assert membership.)
	assert.True(t, true) // replaced by real assertions per the harness
}
```

> **Implementer note:** make the stuck computation a pure, testable function so this test does not need the full LLM/executor harness. Extract `computeStuck(verdicts []examiner.FinalVerdict) map[string]bool` and test it directly: given the two histories above, it returns `/users` stuck and not `/login`. Then `eligibleFailures` consumes that map (already does). This is the cleanest seam.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestNoProgress_SameHintRefailDropsTarget -v`
Expected: FAIL — `computeStuck` undefined (stuck map is always empty in Task 4).

- [ ] **Step 3: Implement `computeStuck` + wire into the loop**

In `run_phases_repair.go`:
```go
// computeStuck returns the set of normalized targets that have made no
// progress: a replacement (Replaces != "") that re-failed with the SAME hint as
// its predecessor. A changed hint (e.g. drift->auth) is progress and is NOT
// stuck. The predecessor is found by walking Replaces to the prior verdict.
func computeStuck(verdicts []examiner.FinalVerdict) map[string]bool {
	byCaseID := map[string]examiner.FinalVerdict{}
	for _, v := range verdicts {
		if v.StepResult.TestCase != nil {
			byCaseID[v.StepResult.TestCase.ID] = v
		}
	}
	stuck := map[string]bool{}
	for _, v := range verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Replaces == "" || v.Status != examiner.StatusFail || v.RedispatchHint == agent.HintNone {
			continue
		}
		prev, ok := byCaseID[tc.Replaces]
		if !ok {
			continue
		}
		if prev.RedispatchHint == v.RedispatchHint {
			stuck[normalizedStuckKey(tc.Target)] = true
		}
	}
	return stuck
}
```

In `executeRepairLoop`, recompute `stuck` at the top of each round (before eligibility) from the current `rp.verdicts`:
```go
	for round := 1; round <= maxRounds; round++ {
		stuck := computeStuck(rp.verdicts)
		eligible := rp.eligibleFailures(stuck)
		...
```
(Remove the empty `stuck` declaration from Task 4.) Add a one-line note in the loop's package-level comment that the budget backstop is the existing LLM-budget-exhaustion error path: a `DecideWithTools` that hits the budget returns an error, `RepairPlan` returns `(nil, err)`, and the loop breaks via the error branch — no new budget API.

- [ ] **Step 4: Run test to verify GREEN**

Run: `go test ./internal/session/ -run TestNoProgress_SameHintRefailDropsTarget -v`
Expected: PASS. Make the test assert `computeStuck`'s output directly on the two histories.

- [ ] **Step 5: Regression — session package**

Run: `go test ./internal/session/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/run_phases_repair.go internal/session/repair_loop_noprogress_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): no-progress guard — same-hint re-fail drops a target

computeStuck marks a target stuck when its replacement re-fails with the same
RedispatchHint as its predecessor (walked via Replaces); a hint change is
progress. Recomputed each round before eligibility. The token-budget backstop is
inherited: DecideWithTools errors on exhaustion -> RepairPlan errors -> loop
breaks (no new budget API)."
```

---

### Task 6: Recovered wiring for `Replaces`

**Files:**
- Modify: `internal/session/summary.go` (`FromResults`)
- Modify: `internal/session/run_phases_consolidate.go` (`writeEpisodicMemory`, `verdictByNormalizedTarget` / the fallback-skip loops)
- Test: `internal/session/summary_replaces_test.go` (new) + extend consolidate recovered test

**Interfaces:**
- Consumes: `TestCase.Replaces` (Task 1), `StepResult.Status`/`FinalVerdict.Status`.
- Produces: a passed replacement recovers its original target (counted `Recovered`), and the replacement itself is not an independent tally/episodic unit — mirroring FallbackFor, but gated on pass-status (NOT the `Recovered` bool, which is set only by the Agent FallbackFor activation path).

- [ ] **Step 1: Write the failing test**

```go
// internal/session/summary_replaces_test.go
package session_test

import (
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/stretchr/testify/assert"
)

// TestFromResults_ReplacesRecovered: a passed replacement recovers its original
// fail (Recovered++), the original stays a fail (not double-counted as pass),
// and the replacement is not an independent unit (TotalCases excludes it).
func TestFromResults_ReplacesRecovered(t *testing.T) {
	primary := &agent.TestCase{ID: "tc-1", Target: "/users", Method: "GET", Service: "api"}
	rep := &agent.TestCase{ID: "repair-tc-1", Target: "/users", Method: "GET", Service: "api", Replaces: "tc-1"}

	results := []agent.StepResult{
		{TestCase: primary, Status: agent.StepFailed},
		{TestCase: rep, Status: agent.StepPassed},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusPass, StepResult: results[1]},
	}

	s := session.FromResults("g", "http://x", 1, results, verdicts, 0, 0, 0)
	assert.Equal(t, 1, s.Recovered, "passed replacement recovers the primary")
	assert.Equal(t, 1, s.Failed, "primary stays a fail")
	assert.Equal(t, 0, s.Passed, "replacement is not an independent pass unit")
	assert.Equal(t, 1, s.TotalCases, "replacement is not an independent unit")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestFromResults_ReplacesRecovered -v`
Expected: FAIL — today the replacement counts as an independent pass and TotalCases=2; Recovered=0.

- [ ] **Step 3: Add the `Replaces` branch in `FromResults`**

In `internal/session/summary.go`, extend the pairing+skip logic. The replacement is not an independent unit (like a fallback), and a passed replacement recovers its primary:

```go
	// Pair primary<->fallback via TestCase.FallbackFor, and primary<->replacement
	// via TestCase.Replaces. A fallback result is not an independent tally unit; a
	// primary whose fallback recovered is reclassified out of Failed into
	// Recovered. A passed replacement recovers its primary the same way — but
	// gated on pass-status (StepResult.Recovered is set only by the FallbackFor
	// activation path, not for Replaces).
	recoveredPrimaryIDs := map[string]bool{}
	nonUnitResultCount := 0
	for _, r := range results {
		tc := r.TestCase
		if tc == nil {
			continue
		}
		if tc.FallbackFor != "" {
			nonUnitResultCount++
			if r.Recovered {
				recoveredPrimaryIDs[tc.FallbackFor] = true
			}
		} else if tc.Replaces != "" {
			nonUnitResultCount++
			if r.Status == agent.StepPassed {
				recoveredPrimaryIDs[tc.Replaces] = true
			}
		}
	}
	s.TotalCases = len(results) - nonUnitResultCount
```

In both the verdicts-loop and the results-loop (the `if len(verdicts) > 0` / `else` branches), change the skip condition from:
```go
			if tc != nil && tc.FallbackFor != "" {
				continue // fallback result, not a unit
			}
```
to also skip replacements:
```go
			if tc != nil && (tc.FallbackFor != "" || tc.Replaces != "") {
				continue // fallback/replacement result, not an independent unit
			}
```
(The `recoveredPrimaryIDs[tc.ID]` check that follows already handles reclassifying the primary as Recovered — no further change needed there.)

- [ ] **Step 4: Add the `Replaces` skip in consolidate**

In `internal/session/run_phases_consolidate.go`:

(a) `writeEpisodicMemory` — the fallback shares its primary's target; so does a replacement. Change:
```go
		if tc.FallbackFor != "" {
			continue
		}
```
to:
```go
		if tc.FallbackFor != "" || tc.Replaces != "" {
			continue
		}
```

(b) The verdict-by-target loop in `verdictByNormalizedTarget` (the `for _, v := range verdicts` block that skips `tc.FallbackFor != ""`):
```go
		if v.StepResult.TestCase.FallbackFor != "" {
			continue
		}
```
to:
```go
		if tc := v.StepResult.TestCase; tc != nil && (tc.FallbackFor != "" || tc.Replaces != "") {
			continue
		}
```
(Read each site first; keep all other conditions. These functions are shared by run + resume, so both paths get the fix.)

- [ ] **Step 5: Run test to verify GREEN**

Run: `go test ./internal/session/ -run TestFromResults_ReplacesRecovered -v`
Expected: PASS.

- [ ] **Step 6: Regression — session package (esp. existing recovered tests)**

Run: `go test ./internal/session/ -count=1`
Expected: PASS. The FallbackFor recovered tests (A1 Phase 2 / #4) must stay green — the `Replaces` branches are additive and a normal case has `Replaces == ""`.

- [ ] **Step 7: Commit**

```bash
git add internal/session/summary.go internal/session/run_phases_consolidate.go internal/session/summary_replaces_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(#3): Replaces recovered wiring

A passed replacement (TestCase.Replaces != '') recovers its original fail
(Recovered++), gated on pass-status (StepResult.Recovered is FallbackFor-only).
summary.FromResults treats a replacement as a non-independent unit and pairs it
to its primary; consolidate's shared writeEpisodicMemory/verdictByNormalizedTarget
skip replacements like fallbacks (run + resume). FallbackFor paths unchanged."
```

---

### Task 7: Integration + gate

**Files:**
- Test: `internal/session/redispatch_integration_test.go` (new)

**Interfaces:**
- Consumes: Tasks 1–6 (an Examiner that tags an actionable hint; a Scout.RepairPlan that emits a replacement; the loop; the recovered wiring).

- [ ] **Step 1: Write the integration test**

```go
// internal/session/redispatch_integration_test.go
package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRedispatchLoop_EndToEnd: one actionable failure is repaired in-session:
// the Examiner tags endpoint_drift, Scout emits a Replaces replacement, the
// Agent runs it, the Examiner re-judges it pass, and the summary counts the
// original target as recovered. Drives the real executeRepairLoop via the
// session test harness with a mock Scout/Agent/Examiner (or the seam from
// Task 4). Also asserts a same-hint re-fail stops after the no-progress guard.
func TestRedispatchLoop_EndToEnd(t *testing.T) {
	// Build a runPhase whose Examiner returns one fail(endpoint_drift) for the
	// primary and whose repair round (stub Scout -> replacement; stub executor ->
	// pass; stub examiner -> pass) recovers it. Assert:
	//  - one Replaces verdict merged;
	//  - FromResults(...).Recovered == 1, .Failed == 1 (primary stays fail);
	//  - a second actionable failure that re-fails same-hint is NOT re-sent.
	require.True(t, true) // replaced by real assertions per the harness
}
```

> **Implementer note:** mirror the existing `internal/session/reflexion_integration_test.go` harness pattern (it already drives a two-phase session with mock drivers). If full DI is impractical, split into two narrower tests: (a) `executeRepairLoop` one-round merge (Task 4's seam) + recovered summary; (b) `computeStuck` no-progress (Task 5's pure function). The deliverable is: actionable fail → replacement recovered end-to-end, and same-hint re-fail stops. Both must pass through real production code paths (not tautological stubs that set the verdict directly).

- [ ] **Step 2: Run the integration test to verify GREEN**

Run: `go test ./internal/session/ -run TestRedispatchLoop_EndToEnd -v`
Expected: PASS.

- [ ] **Step 3: make check (gate)**

Run: `make check`
Expected: EXIT 0 (fmt + lint + test -race). Fix anything fmt/lint flags in the new files. If `make check` reveals a pre-existing failure unrelated to this change, report it as a concern — do not fix unrelated code.

- [ ] **Step 4: Commit + push**

```bash
git add internal/session/redispatch_integration_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "test(#3): examiner->scout redispatch loop end-to-end

Integration: an actionable (endpoint_drift) failure is repaired in-session —
Scout emits a Replaces replacement, the Agent runs it, the Examiner re-judges it
pass, and the original target counts as recovered. A same-hint re-fail stops via
the no-progress guard. make check EXIT 0.

Spec: cerberus-docs/superpowers/specs/2026-07-29-examiner-redispatch-loop-design.md"
git push origin main
```

---

## Self-Review

**Spec coverage:**
- RedispatchHint enum + judge tool schema/prompt + Reasoning reuse → Tasks 1–2. ✓
- `TestCase.Replaces` (new, distinct from FallbackFor) → Task 1. ✓
- `Scout.RepairPlan` + `assembleRepair` + repair prompt + `repairTools` → Task 3. ✓
- Session-orchestrated `executeRepairLoop` between Examiner and Consolidate; shared `buildAgentLoop`/`buildExaminer`; subset run = `ExecutePlan(&TestPlan{Cases: replacements})`; re-judge = `Examine(subset)`; round cap default 2 → Task 4. ✓
- Triple termination: round cap (Task 4) + no-progress same-hint re-fail (Task 5) + inherited token-budget backstop (Task 5 note) → ✓
- Recovered wiring for Replaces (summary + consolidate, gated on pass-status, not the Recovered bool) → Task 6. ✓
- Error handling / repair-never-aborts (every loop error logs + breaks) → Task 4 Step 5. ✓
- Resume idempotency: SavePlan upsert + PersistFinalVerdicts + shared consolidate functions (run+resume) → Tasks 4 & 6. ✓
- Out of scope (structured correction payload, cross-session learning, auth-flow deterministic fallback, non-endpoint special-casing) → untouched. ✓

**Placeholder scan:** No TBD/TODO in code steps. Two implementer-judgment calls are spelled out with both options: (a) Task 4 Step 1 — full DI vs. a `repairPlanFn` seam vs. observable-effect assertions (all three forms named, same contract); (b) Task 4 Step 5 — `RepairPlan` model param (drop if unused) and `normalizedStuckKey` (`memory.NormalizeTarget`). Task 5 Step 1 extracts `computeStuck` as a pure function so the no-progress test avoids the LLM harness. Task 7 gives a fallback decomposition. All named, not deferred.

**Type consistency:**
- `agent.RedispatchHint` (`HintNone`/`HintEndpointDrift`/`HintAuth`/`HintShape`) defined Task 1, used Tasks 2–5. ✓
- `agent.TestCase.Replaces string` defined Task 1, used Tasks 3 (set), 5 (`tc.Replaces` walk), 6 (summary/consolidate). ✓
- `scout.RepairInput{Case, Hint, Reasoning}` defined Task 3, used Task 4 (`eligibleFailures` returns `[]scout.RepairInput`; `RepairPlan` consumes). ✓
- `scout.RepairPlan(ctx, goal, model, failures)` — Task 4 notes the `model` param may be dropped if `buildRepairPrompt` (Task 3) doesn't use it; the call site and signature must stay aligned. (Flagged for the implementer; one place to reconcile.) ✓
- `assembleRepair(calls, failures)`, `repairCaseFromCall`, `repairTools`, `computeStuck(verdicts)`, `eligibleFailures(stuck)`, `buildAgentLoop()`, `buildExaminer()` — names match across tasks. ✓
- `FromResults` signature unchanged; Task 6 only edits its body. ✓

**Scope check:** single cohesive feature (the in-session loop); 7 tasks, each independently testable. No sub-project split needed.

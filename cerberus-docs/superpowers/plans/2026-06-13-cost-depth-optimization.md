# Cost & Depth Optimization — Phase 0+1 Implementation Plan
> **Scope note:** This plan covers **Phase 0 (detect package) + Phase 1 (three-tier model mapping)** — the cost-optimization foundation. Phases 2–4 (Examiner parallelization, ToT params + dual driver, Reflexion injection) are listed as follow-on plans at the end; each is an independent subsystem per the spec `docs/superpowers/specs/2026-06-13-cost-depth-optimization-design.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cerberus read the host CLI's three model tiers (`ANTHROPIC_DEFAULT_{HAIKU,SONNET,OPUS}_MODEL`) and route each head to the tier matching its task complexity, so the high-frequency Agent runs on a cheap tier while Scout/Examiner/Critic keep stronger tiers.

**Architecture:** `internal/detect/` resolves CLI identity → `Profile`. `config.Load` runs detection once, reads the three tier envs from the settings map, and produces a `TierModels` map (head→model) consumed by `session.SetupHeadDrivers`. Per-head resolution priority: explicit `settings.models.<head>` > tier assignment > `ai_budget.model` > built-in default. `CLIUnknown` falls back to today's sonnet-only behavior, unchanged.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), existing `internal/config`, `internal/project`, `internal/session`, new `internal/detect`.

**Constraints:** Commit author `binoctal <binoctal@gmail.com>`, no `Co-Authored-By`. Code comments and commit messages in English. No CGo.

---

## Prerequisites

- [x] **Step 0.1: Clean the working tree**

The working tree currently holds uncommitted v0.7.x changes (Claude Code binding: `internal/config/settings.go`, `internal/llm/*`, `internal/server/*`, `internal/mcp/server.go`, `cmd/cerberus/main.go`; plus the BuildAction sandbox fix `internal/head/agent/multi.go` + tests). Commit or stash these before starting so Phase 0/1 changes are isolated.

```bash
git status --short
# Review the listed files. Commit them as the v0.7.x delivery, e.g.:
git add -A
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat: deep-integrate Claude Code LLM config reuse (v0.7.x)"
```

If any of those changes are unfinished, `git stash` instead and restore later.

---

## Task 0: Land the `internal/detect/` package (Phase 0)

**Files:**
- Create: `internal/detect/detect.go`, `internal/detect/detect_test.go`
- Reference: `docs/superpowers/plans/2026-06-13-provider-detection.md` (the approved Phase 0 plan)

This task is the already-approved provider-detection plan, executed verbatim. It is reproduced in compact form here so this plan is self-contained; if the two ever disagree, the provider-detection plan is authoritative for the `detect` package.

- [x] **Step 0.2: Write the failing test**

`internal/detect/detect_test.go`:
```go
package detect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeCodeDetector_Hit(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.True(t, ok)
	assert.Equal(t, CLIClaudeCode, p.CLI)
	assert.Equal(t, "anthropic", p.Provider)
	assert.Equal(t, "ANTHROPIC", p.EnvPrefix)
}

func TestClaudeCodeDetector_Miss(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.False(t, ok)
	assert.Equal(t, CLIUnknown, p.CLI)
}

func TestDetect_RegistryFirstHitWins(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p := Detect()
	assert.Equal(t, CLIClaudeCode, p.CLI)
}

func TestDetect_AllMiss_ReturnsUnknown(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p := Detect()
	assert.Equal(t, CLIUnknown, p.CLI)
}
```

- [x] **Step 0.3: Run test to verify it fails**

Run: `go test ./internal/detect/ -run TestClaudeCodeDetector`
Expected: FAIL — package `detect` does not exist / `ClaudeCodeDetector` undefined.

- [x] **Step 0.4: Implement the package**

`internal/detect/detect.go`:
```go
// Package detect resolves which CLI hosts cerberus (Claude Code today; Codex /
// Gemini CLI later). The CLI identity is the source of truth for the LLM
// provider: Claude Code always speaks the Anthropic protocol, regardless of the
// model name configured underneath.
package detect

import "os"

// CLI identifies the host command-line environment.
type CLI string

const (
	CLIClaudeCode CLI = "claude-code"
	CLIUnknown    CLI = "unknown"
)

// Profile bundles everything a detected CLI deterministically implies.
type Profile struct {
	CLI          CLI
	Provider     string // "anthropic" | "openai" | "gemini"
	SettingsFile string // e.g. ".claude/settings.json"
	EnvPrefix    string // "ANTHROPIC" | "OPENAI" | "GEMINI"
}

// Detector recognizes one CLI. Detect returns the profile and true when this
// detector's CLI is the active host.
type Detector interface {
	Detect() (Profile, bool)
}

// ClaudeCodeDetector recognizes Claude Code via the CLAUDECODE env var that
// Claude Code injects into every subprocess. Single signal, cross-platform —
// no process-tree walking.
type ClaudeCodeDetector struct{}

func (ClaudeCodeDetector) Detect() (Profile, bool) {
	if os.Getenv("CLAUDECODE") != "" {
		return Profile{
			CLI:          CLIClaudeCode,
			Provider:     "anthropic",
			SettingsFile: ".claude/settings.json",
			EnvPrefix:    "ANTHROPIC",
		}, true
	}
	return Profile{CLI: CLIUnknown}, false
}

// detectors is the ordered registry. First hit wins. Append new detectors here
// to support additional CLIs; no other file needs to change.
var detectors = []Detector{
	ClaudeCodeDetector{},
}

// Detect runs the detector registry and returns the first hit, or an unknown
// profile when no detector matches.
func Detect() Profile {
	for _, d := range detectors {
		if p, ok := d.Detect(); ok {
			return p
		}
	}
	return Profile{CLI: CLIUnknown}
}
```

- [x] **Step 0.5: Run test to verify it passes**

Run: `go test ./internal/detect/ -v`
Expected: PASS (4 tests).

- [x] **Step 0.6: Commit**

```bash
git add internal/detect/
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(detect): add CLI identity detection package"
```

---

## Task 1: `config.Head` type + `resolveTierModels` (Phase 1 core)

**Files:**
- Create: `internal/config/tier.go`
- Create: `internal/config/tier_test.go`

- [x] **Step 1.1: Write the failing test**

`internal/config/tier_test.go`:
```go
package config

import (
	"testing"

	"github.com/binoctal/cerberus/internal/detect"
	"github.com/stretchr/testify/assert"
)

func TestResolveTierModels_ClaudeCode_AssignsByComplexity(t *testing.T) {
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-4-flash",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-5.2",
	}
	got := resolveTierModels(detect.CLIClaudeCode, settings)
	assert.Equal(t, "glm-4-flash", got[HeadAgent], "Agent runs on the fast tier")
	assert.Equal(t, "glm-5.1", got[HeadScout], "Scout plans on the mid tier")
	assert.Equal(t, "glm-5.1", got[HeadExaminer], "Examiner judges on the mid tier")
	assert.Equal(t, "glm-5.2", got[HeadCritic], "Critic reviews on the strong tier")
}

func TestResolveTierModels_UnknownCLI_Empty(t *testing.T) {
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
	}
	got := resolveTierModels(detect.CLIUnknown, settings)
	assert.Empty(t, got, "CLIUnknown leaves tier resolution to the existing logic")
}

func TestResolveTierModels_MissingTierLeavesEmpty(t *testing.T) {
	// Only SONNET set; HAIKU/OPUS unset → Agent/Critic map to "" (caller falls back).
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
	}
	got := resolveTierModels(detect.CLIClaudeCode, settings)
	assert.Equal(t, "glm-5.1", got[HeadScout])
	assert.Equal(t, "", got[HeadAgent], "missing HAIKU tier → Agent falls back to global")
	assert.Equal(t, "", got[HeadCritic], "missing OPUS tier → Critic falls back to global")
}
```

- [x] **Step 1.2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestResolveTierModels`
Expected: FAIL — `HeadAgent`/`resolveTierModels` undefined.

- [x] **Step 1.3: Implement**

`internal/config/tier.go`:
```go
package config

import "github.com/binoctal/cerberus/internal/detect"

// Head identifies a cerberus head that consumes its own LLM driver.
type Head string

const (
	HeadScout    Head = "scout"
	HeadAgent    Head = "agent"
	HeadExaminer Head = "examiner"
	HeadCritic   Head = "critic"
)

// TierModels maps each head to a model selected from the host CLI's tier envs.
// A head mapped to "" has no tier assignment; the caller applies the global
// model / built-in default fallback.
type TierModels map[Head]string

// resolveTierModels assigns each head a model tier by task complexity. Tiers
// are read from the settings map that settings.go populates from the host CLI's
// env block. Only Claude Code declares these tiers today; any other CLI yields
// an empty map so the existing sonnet-only resolution is preserved.
func resolveTierModels(cli detect.CLI, settings map[string]string) TierModels {
	if cli != detect.CLIClaudeCode {
		return TierModels{}
	}
	haiku := settings["ANTHROPIC_DEFAULT_HAIKU_MODEL"]
	sonnet := settings["ANTHROPIC_DEFAULT_SONNET_MODEL"]
	opus := settings["ANTHROPIC_DEFAULT_OPUS_MODEL"]
	return TierModels{
		// Execution is frequent and mechanical → fast tier.
		HeadAgent: haiku,
		// Planning and judgment carry quality weight → mid tier.
		HeadScout:    sonnet,
		HeadExaminer: sonnet,
		// Low-frequency high-stakes review → strong tier.
		HeadCritic: opus,
	}
}
```

- [x] **Step 1.4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestResolveTierModels -v`
Expected: PASS (3 tests).

- [x] **Step 1.5: Commit**

```bash
git add internal/config/tier.go internal/config/tier_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(config): map heads to CLI model tiers by task complexity"
```

---

## Task 2: Wire detection + tiers into `config.Load`

**Files:**
- Modify: `internal/config/config.go` (add `CLIProfile` + `TierModels` to `Config`; call `detect.Detect()` and `resolveTierModels` in `Load`)
- Modify: `internal/config/config_test.go` (add priority coverage)

- [x] **Step 2.1: Read current `Load` and `Config`**

Run: `sed -n '1,40p' internal/config/config.go`
Confirm the `Config` struct fields and `Load()` body so the edit below matches exactly.

- [x] **Step 2.2: Write the failing test**

Append to `internal/config/config_test.go`:
```go
func TestLoad_TierModelsUnderClaudeCode(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	// Minimal settings file is not required: tier envs come from settings.go's
	// map, but Load reads the real .claude/settings.json. For a deterministic
	// unit test, call resolveTierModels directly (covered in tier_test.go) and
	// assert Load populates CLIProfile under CLAUDECODE.
	cfg := Load()
	assert.Equal(t, detect.CLIClaudeCode, cfg.CLIProfile.CLI)
}

func TestLoad_UnknownCLI_NoTierModels(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	cfg := Load()
	assert.Equal(t, detect.CLIUnknown, cfg.CLIProfile.CLI)
	assert.Empty(t, cfg.TierModels)
}
```

> Note: `Load` reads the real `.claude/settings.json` via `loadClaudeCodeEnv`. These two tests assert only the detection-driven fields (`CLIProfile`, and that `TierModels` is empty when CLI is unknown), which are deterministic via `t.Setenv`. The tier *contents* are unit-tested in `tier_test.go` against an explicit settings map, avoiding filesystem coupling here.

- [x] **Step 2.3: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoad_`
Expected: FAIL — `cfg.CLIProfile` undefined.

- [x] **Step 2.4: Implement**

In `internal/config/config.go`, add the import and two fields to `Config`, and call detection in `Load`. Concretely:

1. Add `"github.com/binoctal/cerberus/internal/detect"` to the import block.
2. Add two fields to the `Config` struct:
```go
	CLIProfile detect.Profile // resolved host CLI identity
	TierModels TierModels     // head → model tier (empty when CLI unknown)
```
3. In `Load()`, after `settings := loadClaudeCodeEnv()`, resolve the profile and tiers, and assign them to the returned config. The existing `LLMProvider`/`LLMModel`/`LLMAPIKey` resolution stays as-is for now (provider-priority refinement belongs to the provider-detection plan; this task only adds tier data):
```go
func Load() *Config {
	settings := loadClaudeCodeEnv()
	profile := detect.Detect()
	cfg := &Config{
		// ...existing fields unchanged (LLMModel, LLMProvider, etc.)...
		CLIProfile: profile,
	}
	cfg.TierModels = resolveTierModels(profile.CLI, settings)
	// ...existing resolveAPIKey etc. unchanged...
	return cfg
}
```

- [x] **Step 2.5: Run test to verify it passes**

Run: `go test ./internal/config/ -v`
Expected: PASS — all existing config tests plus the two new ones.

- [x] **Step 2.6: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(config): populate CLIProfile and TierModels in Load"
```

---

## Task 3: Route heads to tiers in `SetupHeadDrivers`

**Files:**
- Modify: `internal/session/lifecycle.go` (`SetupHeadDrivers` uses `TierModels` with the explicit > tier > global priority chain)
- Modify or add: `internal/session/lifecycle_test.go` (priority table)

- [x] **Step 3.1: Re-read `SetupHeadDrivers`**

Run: `sed -n '88,135p' internal/session/lifecycle.go`
Confirm the current `models := s.Config.Settings.Models` + `heads` slice + `driverFor` fallback so the edit matches.

- [x] **Step 3.2: Write the failing test**

If `internal/session/lifecycle_test.go` does not exist, create it. Add a priority test that constructs a `Session` with a `Config` carrying `TierModels` + explicit `Models`, and asserts which model each head's driver was built with. Because `SetupHeadDrivers` builds real LLM clients (network-free construction, no dial), assert via the configured model captured on the driver — expose it through a small read helper if none exists, else assert via logs/counts. Minimal form:

```go
package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/detect"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/assert"
)

// TestSetupHeadDrivers_Priority verifies explicit settings.models wins over
// tier assignment, which wins over the global ai_budget.model.
func TestSetupHeadDrivers_Priority(t *testing.T) {
	s := &Session{
		Config: &Config{
			Settings: project.Settings{
				Models:    project.Models{Agent: "explicit-agent"},
				AIBudget:  project.AIBudget{Model: "global-model"},
			},
		},
		// TierModels injected directly to avoid filesystem/env coupling.
	}
	s.Config.CLIProfile = detect.Profile{CLI: detect.CLIClaudeCode}
	s.Config.TierModels = config.TierModels{
		config.HeadAgent:    "tier-haiku",
		config.HeadScout:    "tier-sonnet",
		config.HeadExaminer: "tier-sonnet",
		config.HeadCritic:   "tier-opus",
	}

	s.SetupHeadDrivers("test-key", "http://example")

	// Agent: explicit "explicit-agent" wins over tier "tier-haiku".
	// Scout: no explicit → tier "tier-sonnet".
	// (Driver model introspection: if no public accessor, assert that the
	// per-head driver field is non-nil for configured heads and nil when no
	// model resolves. Refine the assertion to the accessor this codebase
	// exposes; the priority *logic* is what this test locks in.)
	assert.NotNil(t, s.agentDriver, "Agent driver built from explicit model")
	assert.NotNil(t, s.scoutDriver, "Scout driver built from tier model")
}
```

> The exact assertion depends on how `ai.Driver` exposes its model. During implementation, if `ai.Driver` has no model accessor, add a `Model() string` method on `Driver` (one-liner returning the stored client model) so the priority is testable — this is a justified, minimal testability hook, not production logic.

- [x] **Step 3.3: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestSetupHeadDrivers_Priority`
Expected: FAIL — current `SetupHeadDrivers` ignores `TierModels`, so Scout would not be built from the tier.

- [x] **Step 3.4: Implement**

Rewrite `SetupHeadDrivers` in `internal/session/lifecycle.go` to apply the priority chain. Replace the current body (from `models := s.Config.Settings.Models` through the end of the `heads` loop) with:

```go
func (s *Session) SetupHeadDrivers(apiKey, baseURL string) {
	tier := s.Config.TierModels
	globalModel := s.Config.Settings.AIBudget.Model

	// pick resolves a head's model: explicit project.yaml override, then the
	// detected CLI tier, then the global ai_budget.model.
	pick := func(head config.Head, explicit string) string {
		if explicit != "" {
			return explicit
		}
		if m, ok := tier[head]; ok && m != "" {
			return m
		}
		return globalModel
	}

	type head struct {
		head     config.Head
		explicit string
		field    **ai.Driver
	}
	heads := []head{
		{config.HeadScout, s.Config.Settings.Models.Scout, &s.scoutDriver},
		{config.HeadAgent, s.Config.Settings.Models.Agent, &s.agentDriver},
		{config.HeadExaminer, s.Config.Settings.Models.Examiner, &s.examinerDriver},
		{config.HeadCritic, s.Config.Settings.Models.Critic, &s.criticDriver},
	}

	for _, h := range heads {
		m := pick(h.head, h.explicit)
		if m == "" {
			continue // nothing resolves → fall back to shared Driver via driverFor
		}
		client, err := llm.NewClientWithConfig(llm.ClientConfig{
			Model:   m,
			APIKey:  apiKey,
			BaseURL: baseURL,
		})
		if err != nil {
			s.Logger.Warn("failed to create head driver, using shared",
				zap.String("model", m), zap.Error(err))
			continue
		}
		budget := ai.NewTokenBudget(
			s.Config.Settings.AIBudget.SessionTotalTokens,
			s.Config.Settings.AIBudget.PerCallLimit,
		)
		*h.field = ai.NewDriver(client, budget)
		s.Logger.Info("head driver configured",
			zap.String("head", string(h.head)),
			zap.String("model", m))
	}
}
```

Add `"github.com/binoctal/cerberus/internal/config"` to the import block if not present.

- [x] **Step 3.5: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestSetupHeadDrivers_Priority -v`
Expected: PASS.

- [x] **Step 3.6: Run the full suite to catch regressions**

Run: `go test ./...`
Expected: PASS — `driverFor` still returns the shared `Driver` for heads with no resolved model, so existing single-model behavior is unchanged when no tiers/overrides are set.

- [x] **Step 3.7: Commit**

```bash
git add internal/session/lifecycle.go internal/session/lifecycle_test.go
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "feat(session): route heads to model tiers with explicit>tier>global priority"
```

---

## Task 4: Documentation + end-to-end verification

**Files:**
- Modify: `docs/configuration/project.md` (document tier auto-assignment)

- [x] **Step 4.1: Document tier auto-assignment**

In `docs/configuration/project.md`, add a subsection under the models/LLM config area:

```markdown
### Automatic model tier assignment

When cerberus runs under Claude Code, it reads three model tiers from
`.claude/settings.json` and routes each head to the tier matching its task
complexity — no per-head config required:

| Env var | Tier | Used by |
|---|---|---|
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | fast | Agent (ReAct execute) |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | mid | Scout (plan), Examiner (judge) |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | strong | Critic (review) |

Per-head `settings.models.<head>` overrides still win when set; `CLIUnknown`
(standalone runs) keeps the previous single-model behavior. To enable tiering,
set the three `ANTHROPIC_DEFAULT_*_MODEL` values in your settings.json — e.g.
point HAIKU at a cheaper model for the high-frequency Agent.
```

- [x] **Step 4.2: `make check`**

Run: `make check`
Expected: fmt clean, lint clean, all tests pass.

- [x] **Step 4.3: Manual smoke test (optional, requires Claude Code env)**

Under Claude Code with the three tiers set in `.claude/settings.json`, run:
```bash
go run ./cmd/cerberus run --goal "health endpoint returns 200" 2>&1 | grep "head driver configured"
```
Expected: log lines show Agent on the HAIKU-tier model, Scout/Examiner on SONNET-tier, Critic on OPUS-tier.

- [x] **Step 4.4: Commit**

```bash
git add docs/configuration/project.md
git -c user.name="binoctal" -c user.email="binoctal@gmail.com" \
  commit -m "docs: document automatic model tier assignment under Claude Code"
```

---

## Self-Review (completed)

- **Spec coverage**: Phase 0 (`internal/detect/`) = Task 0; Phase 1 (tier source + assignment + priority + fallback) = Tasks 1–4. All four Phase-1 rows of the spec's assignment table are locked in by `TestResolveTierModels_ClaudeCode_AssignsByComplexity`. Priority chain locked by `TestSetupHeadDrivers_Priority`. `CLIUnknown` fallback locked by `TestResolveTierModels_UnknownCLI_Empty` + `TestLoad_UnknownCLI_NoTierModels`.
- **Placeholder scan**: one open item is the exact model-introspection accessor for the Task 3 assertion (`ai.Driver.Model()` may need adding). It is flagged inline as a minimal testability hook with concrete guidance, not left as "TBD".
- **Type consistency**: `config.Head` / `config.TierModels` defined in Task 1, consumed unchanged in Tasks 2–3. `detect.Profile` / `detect.CLI` from Task 0 used in Tasks 1–2.
- **Default preservation**: when `CLI == CLIUnknown` or tiers are unset, `TierModels` is empty and `pick` returns the global model, so pre-existing single-model runs are byte-for-byte unchanged — verified by Task 3.6 full-suite run.

---

## Follow-on plans (Phases 2–4, each independent)

These are out of scope for this plan but tracked here so nothing is lost. Each becomes its own plan file when prioritized.

- **Phase 2 — Examiner parallelization** (`internal/head/examiner/examiner.go`): replace the `for _, r := range results` loop in `Examine` with a worker pool (semaphore + `sync.WaitGroup`, writing verdicts by index to preserve order), modeled on `agent.ParallelExecutor` but without the dependency graph (Judge calls are independent). Add `MaxWorkers` to `ExaminerConfig`. Saves wall-clock, not tokens.

- **Phase 3 — ToT params + dual driver** (`internal/project/schema.go`, `internal/config`, `internal/head/scout/tot.go`): add `Settings.ToT{beam_width,generate_n,max_steps}` + `Settings.Reflexion{episodic_limit,semantic_topk,semantic_threshold}` to the schema (defaults 3/5/3 and 10/5/0.3); add `resolveToTConfig`/`resolveReflexionConfig` in config; give `ToTPlanner` a `proposeDriver` (SONNET tier) and `evaluateDriver` (HAIKU tier) — the tier principle from Phase 1 applied to ToT's two subtasks; update `SetDeepPlan` to accept both; expand `ToTConfig` comments (already done in this session) and add `docs/configuration/tot.md`.

- **Phase 4 — Reflexion injection into ToT** (`internal/head/scout/tot.go`, `scout.go`): prepend the output of `buildEpisodicContext` (`scout.go:338`) to `ToTPlanner.propose`'s prompt, parameterized by `Settings.Reflexion`. `evaluate` stays a pure scoring step. Closes the mutual-exclusion gap where ToT mode discards cross-session memory.

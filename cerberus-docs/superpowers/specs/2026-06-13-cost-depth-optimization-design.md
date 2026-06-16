# Cost & Depth Optimization — Design Spec

**Date**: 2026-06-13
**Status**: Draft (pending user review)

## Goal

Two correlated outcomes, unified under one mechanism — a three-tier model
profile derived from the host CLI:

1. **Cost**: run high-frequency / mechanical heads on a cheap model tier and
   judgment-heavy heads on stronger tiers, instead of one model for everything.
2. **Depth**: expose the currently hardcoded Tree-of-Thought and Reflexion
   parameters so planning depth and memory recall become tunable, and close the
   gap where ToT mode silently discards cross-session memory.

The host CLI's settings already declare three model tiers
(`ANTHROPIC_DEFAULT_{HAIKU,SONNET,OPUS}_MODEL`); cerberus currently reads only
the SONNET tier. This spec makes the CLI identity (from the `internal/detect/`
package, Phase 0) the source of truth for which tiers exist, and routes each
head to the tier matching its task complexity.

## Context / Problem

Five independent deficiencies, each verified in code:

- **Single-tier model use**: `config.resolveModel` reads only
  `ANTHROPIC_DEFAULT_SONNET_MODEL` (`config.go:36`); the HAIKU and OPUS tiers
  present in `.claude/settings.json` are ignored. Every head shares one model,
  so the most frequent caller (Agent ReAct — multi-turn per case × N cases)
  pays the full strong-model rate for mechanical work.
- **Hardcoded ToT params**: `ToTConfig{BeamWidth:3, GenerateN:5, MaxSteps:3}`
  is fixed in `DefaultToTConfig()` (`tot.go:24`); `--deep-plan` only toggles
  ToT on/off with this fixed config. No project.yaml or flag can tune depth.
- **Reflexion params hardcoded**: `buildEpisodicContext` (`scout.go:338`) bakes
  in L1 episodic limit 10, L2 semantic top-5 @ 0.3 similarity. Untunable.
- **ToT mode loses memory**: `Plan()` (`scout.go:107`) branches — the ToT path
  calls `planner.Plan()` and returns; only `directPlan` (`scout.go:146`) calls
  `buildEpisodicContext`. So deeper planning via ToT *discards* cross-session
  lessons — the two "depth" mechanisms are mutually exclusive today.
- **Examiner is sequential**: `Examiner.Examine` (`examiner.go:38`) judges
  results in a plain `for` loop; unlike the Agent's `ParallelExecutor`, no
  concurrency, so many-case runs pay full serial wall-clock.

## Design Principles

1. **CLI identity is the source of truth for tiers** — Claude Code declares
   HAIKU/SONNET/OPUS; detection reads them. No new config field for tiers.
2. **Match tier to task complexity** — execution cheap, judgment mid, review
   strong. One principle, applied per-head and per-subtask.
3. **Pure additive, defaults unchanged** — new knobs exist but default to
   today's values; standalone / non-Claude-Code runs are byte-for-byte
   unchanged.
4. **Explicit > detected > fallback** — explicit `settings.models` always wins;
   tier auto-assignment fills the gaps; `CLIUnknown` falls back to sonnet-only.

## Phase 0 — `internal/detect/` package

Execute the already-approved plan
`docs/superpowers/plans/2026-06-13-provider-detection.md`. Delivers CLI
identity detection (`CLAUDECODE` env → `Profile{CLI, Provider, EnvPrefix}`).
No work duplicated here; Phase 1 builds on the resulting `Profile`.

Prerequisite: the working tree currently has uncommitted v0.7.x changes
(Claude Code binding + BuildAction sandbox fix + tests); commit/stash before
starting.

## Phase 1 — Three-tier model mapping (cost core)

### Source
`detect.Detect()` returns `Profile`; when `CLI == CLIClaudeCode`, cerberus
reads three tiers from the settings map (already loaded wholesale by
`settings.go`):

- `ANTHROPIC_DEFAULT_HAIKU_MODEL`  → fast tier
- `ANTHROPIC_DEFAULT_SONNET_MODEL` → mid tier
- `ANTHROPIC_DEFAULT_OPUS_MODEL`   → strong tier

### Assignment (by task complexity)

| Head / subtask | Tier | Rationale |
|---|---|---|
| Agent (ReAct execute) | HAIKU | highest frequency, most mechanical |
| Scout directPlan | SONNET | planning quality |
| Examiner Judge | SONNET | correctness judgment |
| Scout ToT propose | SONNET | strategy generation |
| Scout ToT evaluate | HAIKU | scoring, non-generative |
| Critic | OPUS | low-frequency high-stakes review |

### Resolution priority (per head)

```
1. settings.models.<head>        (explicit project.yaml override)
2. tier assigned by complexity   (this phase)
3. ai_budget.model               (global fallback)
4. built-in default
```

### Fallback
`Profile.CLI == CLIUnknown` → each head falls back to the existing
sonnet-only resolution (current behavior, unchanged).

### Code
- `config.go`: replace the single `resolveModel` with per-tier resolution; add
  `resolveTierModels(profile, settings) map[head]string`. `Config` gains a
  `TierModels` map consumed by `SetupHeadDrivers`.
- `lifecycle.go` `SetupHeadDrivers`: iterate heads, pick model from
  `TierModels[head]` (then explicit `models.<head>`, then global), build
  per-head client + budget.

## Phase 2 — Examiner parallelization

`Examiner.Examine` (`examiner.go:38`) replaces its `for _, r := range results`
loop with the existing `agent.ParallelExecutor` pattern (worker pool;
`MaxWorkers` reused from session config, default 4). Judge calls are
independent per case (no cross-case state until aggregation), so they
parallelize cleanly.

Reflexion `Learn` runs once after all verdicts (unchanged — it is an
aggregation step and must see all results).

This saves wall-clock, not tokens. No model change.

## Phase 3 — ToT parameters exposed + dual driver (depth core)

### Parameter semantics

(to be documented in `tot.go` `ToTConfig` comments — single source of truth —
and `docs/configuration/tot.md`)

Beam search explores a strategy tree along three orthogonal dimensions:

| Field | Acts at | Constrains | Dimension |
|---|---|---|---|
| `GenerateN` | propose | # children expanded per surviving parent | breadth |
| `BeamWidth` | select | # top candidates kept after pruning | survivors |
| `MaxSteps` | loop | # propose→evaluate→select refinement rounds | depth |

`GenerateN` = "how many you make"; `BeamWidth` = "how many you keep".
`MaxSteps` is iterative refinement (each round re-proposes from the
survivors), not a plain N-level tree expansion. Per-step evaluate cost scales
with `BeamWidth × GenerateN`.

Tuning guide: raise `MaxSteps` for depth (linear cost), `GenerateN` for
breadth, `BeamWidth` to avoid pruning good strategies (most expensive — it
compounds every subsequent step).

### Config
project.yaml gains (all optional, defaults = today's values):

```yaml
settings:
  tot:       {beam_width: 3, generate_n: 5, max_steps: 3}
  reflexion: {episodic_limit: 10, semantic_topk: 5, semantic_threshold: 0.3}
```

### Dual driver
`ToTPlanner` takes two drivers: `proposeDriver` (SONNET tier) and
`evaluateDriver` (HAIKU tier). This is the same tier principle from Phase 1
applied to ToT's two subtasks. `SetDeepPlan` accepts both.

### Documentation
- Expand `ToTConfig` struct comments in `tot.go` (single source of truth).
- Add `docs/configuration/tot.md` explaining the three knobs + cost trade-offs.

## Phase 4 — Reflexion memory injected into ToT (closes mutual-exclusion gap)

`ToTPlanner.propose` prepends episodic + semantic memory to its prompt context,
mirroring `directPlan.buildEpisodicContext` (`scout.go:338`). Memory parameters
(`episodic_limit`, `semantic_topk`, `semantic_threshold`) come from the
`settings.reflexion` block (Phase 3).

`evaluate` stays a pure scoring step (no memory) — it runs on the cheap HAIKU
tier and should not carry large context.

Result: ToT mode no longer loses cross-session memory. The two depth
mechanisms compose instead of excluding.

## Dependency chain

```
Phase 0 (detect) → Phase 1 (tier mapping) → { Phase 2 (examiner parallel),
                                               Phase 3 (tot params + dual driver) }
                                       Phase 3 → Phase 4 (reflexion injection)
```

Each phase is independently shippable and testable.

## Testing

- Phase 0: per the existing plan.
- Phase 1: priority table — explicit `models` > tier > `ai_budget` > default;
  `CLIUnknown` → sonnet-only. `t.Setenv` for `CLAUDECODE` + tier envs.
- Phase 2: `Examine` with N synthetic results yields the same verdicts as
  serial (order-independent); `MaxWorkers` honored.
- Phase 3: `resolveToTConfig` reads `settings.tot`; missing fields fall back to
  3/5/3. Dual driver: propose uses SONNET-tier client, evaluate uses HAIKU-tier.
- Phase 4: ToT propose prompt contains episodic/semantic context when memory is
  present; empty memory → prompt unchanged (no regression).

## Out of Scope (YAGNI)

- Codex / Gemini CLI tier mappings (not verifiable without those CLIs).
- Cost telemetry / per-head token dashboards (observe first, build later).
- Auto-inference of fast/strong from model name (fragile across providers).
- ToT propose + evaluate both injecting memory (token cost; conflicts with the
  HAIKU-tier role of evaluate).
- Lowering default ToT/Reflexion values (defaults unchanged; tuning is opt-in).

## Verification

- `make check` (fmt + lint + test) green per phase.
- Manual: under Claude Code with three tiers set, `cerberus run --deep-plan`
  uses HAIKU for Agent + ToT evaluate, SONNET for Scout/Examiner, OPUS for
  Critic; the ToT propose prompt includes prior-session lessons; Examiner runs
  concurrently.

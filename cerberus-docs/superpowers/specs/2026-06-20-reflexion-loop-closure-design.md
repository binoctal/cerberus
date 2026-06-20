# Reflexion Learning Loop Closure — Design (v2)

**Date:** 2026-06-20
**Status:** Design (pre-implementation)
**Supersedes:** the initial "build a Recaller + recall/consolidate phases" proposal, which an adversarial review proved was over-built on a false premise (it re-implemented planning recall that already exists) and omitted two fatal gaps.

---

## 1. Goal

Make subsequent `cerberus run` sessions genuinely benefit from earlier sessions: the agent's recovery should recall lessons that actually apply, and their effectiveness should evolve with real outcomes. Memory must not grow without bound, and its state must be inspectable.

**Non-goal:** re-building planning-time recall — Scout already recalls L1 (episodic) and L2 (semantic) memory in both direct and ToT modes (see §3).

---

## 2. Verified current state

Each claim below was confirmed against code by two independent reviewers (fact-check + adversarial).

| Layer | Write path | Read path | Loop status |
|---|---|---|---|
| **L1 episodic** | `store.RecordEpisodic` (`internal/store/memory.go:18`) — **no caller** | `store.GetEpisodicByTarget` in Scout planning (`internal/head/scout/memory_helpers.go:33`) | **Broken** — recall always empty |
| **L2 semantic** | `learner.storeSemanticFromReflections` → `StoreSemantic(..., embedder.ModelName())` (`learner_storage.go:115`) | `querySemanticMemories` → `SearchSemanticForProject` in Scout (`memory_helpers.go:75`) | **Closed** |
| **L3 procedural** | `learner.Learn` → `StoreProceduralWithType` (`learner_run.go`) | `GetProceduralByMatch` in agent recovery (`recovery.go:71`) | **Closed but inert** — see §4.1 |

Supporting facts:
- Examiner `FinalVerdict` carries `StepResult.TestCase.Target`, `StepResult.Duration`, and status in `{pass, fail, skip, uncertain}` (`examiner/policy.go`, `examiner/types.go`) — everything needed to write an episodic record is present at verdict time.
- CLI `run`, `cerberus serve`, and `cerberus mcp` all enter through one shared `Session.Run()` (`internal/session/lifecycle_run.go`). `--resume` uses a **separate** `Session.Resume()` path (`lifecycle_resume.go`).
- `memory_procedural` already has `archived` (migration V002). `memory_episodic` and `memory_semantic` do **not**.
- `memory_semantic` stores `embedding_model` (V004); `memory_procedural` has **no** embedding column.
- Reflexion config defaults: `EpisodicLimit=10`, `SemanticTopK=5`, `SemanticThreshold=0.3` (`scout.go:38`).
- Existing governance helpers already present: `AutoArchiveLowEffectiveness`, `ArchiveProcedural`, `MarkStaleProcedural`.

---

## 3. Why the first design was wrong

The initial proposal assumed "planning reads nothing" and proposed a new `Recaller` component, a recall phase, and goal-level semantic recall. The review proved:
- Scout **already** recalls L1+L2 and injects into both planning modes (`buildEpisodicContext`, `plan_phases.go:64`, `direct_planning.go:92`).
- L2 semantic is already a closed write→read loop.

So the real surface is far smaller — **planning code stays unchanged**. Once L1 is written, planning sees per-target history automatically.

---

## 4. Fatal findings (must fix before any of this is useful)

### 4.1 L3 recall predicate is effectively inert
`GetProceduralByMatch` (`internal/store/procedural_query.go`) loads all non-archived procedural rows with `effectiveness >= 0.2`, then client-side filters with `strings.Contains(target, condition) || strings.Contains(condition, target)`. Conditions are free text (e.g. `"4xx on unauthenticated request"`); targets are endpoint paths (e.g. `/api/v1/auth/login`). They almost never overlap, so **recovery recalls zero L3 memories in practice**, and any effectiveness feedback wired downstream would have nothing to act on.

**Fix — embedding-based L3 recall (parallel to L2):**
- Migration: add `embedding TEXT NOT NULL DEFAULT '[]'` and `embedding_model TEXT NOT NULL DEFAULT ''` to `memory_procedural`.
- At `Learn` time, embed each reflection's condition with the existing embedder and store it alongside the row.
- New `GetProceduralByEmbedding(ctx, queryEmbedding, project, topK, threshold, currentModel)`: load non-archived rows for the project whose `embedding_model` matches the current embedder, cosine-rank, keep those above threshold, re-rank by `effectiveness`, return top-K.
- Recovery embeds its target and calls `GetProceduralByEmbedding` instead of `GetProceduralByMatch`. (Recovery gains an embedder dependency — wire it into the `Recovery` struct.)
- Keep `GetProceduralByMatch` for backward compatibility / CLI, but recovery no longer depends on it.

### 4.2 Effectiveness attribution is broken
Recovery recalls up to 5 L3 memories and only injects their text into the prompt; the LLM may ignore them. The v1 design credited/penalised all 5 by the whole case's verdict, and `Recover` is called once per recovery attempt (`execute_phases_recovery.go:26`), so a failing case with 3 attempts would penalise every recalled memory three times (≈0.66 EMA penalty). Skip verdicts were also counted as failures even though skip means "target unreachable", not "strategy was bad".

**Fix:**
- **Dedup:** at most one EMA update per `(case_id, procedural_id)`, regardless of how many recovery attempts recalled it. Enforced by a `UNIQUE`-ish write at `memory_usage` insertion and by consolidate grouping.
- **Skip excluded:** verdicts of `skip` produce **no** effectiveness update.
- **Success signal:** `success = (final case verdict == pass)`. (A per-attempt `recExecResult`-based signal would be tighter but needs deeper plumbing; recorded as a §10 follow-up. Dedup + skip-exclusion already remove the two fatal pathologies: multi-counting and mis-attribution of unreachable targets.)

---

## 5. Risk findings (must address for correctness/safety)

| # | Risk | Fix |
|---|---|---|
| **C** | `Learn()` runs **before** `PersistFinalVerdicts` (`examiner.go`); a consolidate phase reading verdicts right after `Examine` would see uncommitted/empty verdicts. Archive-then-`Learn` also re-creates just-archived rows (no dedup key). | Consolidate runs as a **separate phase after verdicts are committed**. `Learn` dedups on `condition+action+project` (update existing instead of insert). |
| **D** | Deleting `memory_usage` after consolidate loses debuggability and risks double-EMA on a mid-consolidate crash. | `memory_usage` keeps a `consolidated_at` column; consolidate filters `WHERE consolidated_at IS NULL` and stamps it. Physical delete only via `cerberus memory prune --hard`. |
| **E** | `--resume` re-runs Examiner (`Resume()` path), so episodic writes and effectiveness updates for cases already committed in the original session would be double-applied. | On resume, episodic write + consolidate process **only cases not already committed** by the original session (exclude targets/case_ids present in committed verdicts). |
| **F** | `UpdateProceduralEffectiveness` is read-modify-write with no transaction; `--parallel` workers and concurrent sessions can lose updates. | Replace with an atomic SQL update: `UPDATE memory_procedural SET effectiveness = effectiveness*0.7 + 0.3*?, usage_count = usage_count+1 WHERE id = ?` (α=0.3). Drop the Go-side read. |
| **G** | Filtering recall by `embedding_model` makes every memory invisible on embedder model change — a silent brain-wipe. | Recall filters by current model **and** the spec adds `cerberus memory reembed` to regenerate all embeddings with the current model. |
| **A1** | `queryEpisodicMemories` reads on targets from `extractUniqueTargets` (emits `endpoint.Path`, e.g. `/users/{id}`); `RecordEpisodic` would write the case's actual target (e.g. `/api/users/123`). Shape mismatch keeps recall empty. | A shared `NormalizeTarget` helper applied at **both** the episodic write and the episodic read so the keys agree. |

---

## 6. Design (v2)

### 6.1 L1 episodic write (biggest single win)
In `executeExaminerPhase`, after verdicts are produced, call `RecordEpisodic(sessionID, NormalizeTarget(target), status, verdict, durationMs)` per verdict. Skip verdicts whose case never executed (no target). This makes `buildEpisodicContext` return real per-target history on the next run.

### 6.2 L3 embedding recall (§4.1)
Migration + embedder storage + `GetProceduralByEmbedding`; recovery uses it.

### 6.3 Effectiveness feedback
- Recovery writes `memory_usage(procedural_id, session_id, case_id, target, attempt, created_at, consolidated_at)` when it recalls a memory (dedup so a `(case_id, procedural_id)` pair is written once).
- New **consolidate phase** (after verdicts committed): for each unconsumed `memory_usage` row, look up the case's final verdict; if `skip`, mark consolidated and continue; else apply the **atomic EMA update** (§5-F) with `success = verdict == pass`; then stamp `consolidated_at`.

### 6.4 Consolidate phase (new)
`executeConsolidatePhase()` on `runPhase`, invoked in `lifecycle_run.go` after `executeExaminerPhase`, and in the resume path with §5-E dedup. Responsibilities: (1) apply effectiveness EMA from `memory_usage`+verdicts, (2) archive stale memory, (3) **no deletion**.

### 6.5 Governance
Reuse existing helpers where possible:
- **L3 procedural:** `AutoArchiveLowEffectiveness` — archive when `effectiveness < 0.3 AND usage_count >= 5 AND age > 30d`.
- **L2 semantic:** archive when `age > 90d` (no effectiveness signal) or duplicate content (keep newest).
- **L1 episodic:** archive when `age > 30d` (recent results are what planning needs).
- Migration adds `archived INTEGER NOT NULL DEFAULT 0` to `memory_episodic` and `memory_semantic`; all read queries add `WHERE archived = 0`.

### 6.6 `cerberus memory` CLI
- `list [--type procedural|semantic|episodic] [--project P] [--all]` — table of effectiveness/usage/age; hides archived unless `--all`.
- `show --id N` (uses the existing `GetSemanticByID`/equivalents — **do not delete `GetSemanticByID`**).
- `prune [--dry-run] [--hard]` — soft-archive by default; `--hard` physically deletes.
- `reembed` — regenerate all embeddings with the current embedder model (§5-G).

### 6.7 Dead code
- **Remove:** `MatchStrategies`, `FormatStrategiesForPrompt` (superseded by `buildEpisodicContext`/embedding recall).
- **Keep & wire:** `RecordEpisodic` (§6.1), `UpdateProceduralEffectiveness` (rewritten atomically, §5-F).
- **Keep:** `GetSemanticByID` (CLI `show`).

### 6.8 Planning code
**Substantively unchanged.** L1+L2 recall already wired (`buildEpisodicContext`); no new recall logic is added. The only Scout-side touch is wrapping the episodic read key in the shared `NormalizeTarget` helper (§5-A1) so it matches the write key. Once L1 is written (§6.1), planning sees per-target history with no other Scout changes.

---

## 7. Data model changes (single new migration, atomic, reversible)

```sql
-- L3 embedding recall (§4.1)
ALTER TABLE memory_procedural ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_procedural ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';

-- Archival for L1/L2 (§6.5)
ALTER TABLE memory_episodic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_semantic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

-- Effectiveness feedback (§6.3)
CREATE TABLE IF NOT EXISTS memory_usage (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  procedural_id   INTEGER NOT NULL,
  session_id      TEXT    NOT NULL,
  case_id         TEXT    NOT NULL,
  target          TEXT    NOT NULL,
  attempt         INTEGER NOT NULL,
  created_at      DATETIME NOT NULL,
  consolidated_at DATETIME,
  UNIQUE(case_id, procedural_id)  -- enforces §4.2 dedup
);
CREATE INDEX idx_memory_usage_unconsolidated ON memory_usage(procedural_id) WHERE consolidated_at IS NULL;
```

---

## 8. Error handling
Memory is an enhancement, never a dependency. Any failure in embedding, recall, write, or consolidate logs a warning and degrades: planning proceeds with empty memory; consolidate skips the failing row. Runs never abort due to memory. Migrations are additive with safe defaults.

---

## 9. Testing
- **Unit:** `NormalizeTarget` symmetry; `GetProceduralByEmbedding` ranking (effectiveness × similarity, model filter, archived filter); atomic EMA SQL correctness; consolidate idempotency (`consolidated_at`), skip-exclusion, dedup; archival thresholds per layer; `Learn` dedup on `condition+action`; CLI list/prune/reembed.
- **Integration (mock LLM/embedder):** two-session sequence — session 1 produces reflections + verdicts → episodic written, L3 embedded; session 2's planning prompt contains session-1 per-target history, recovery recalls an L3 by embedding, consolidate updates its effectiveness, a low-effectiveness memory gets archived. Plus a resume test asserting no double-write for already-committed cases.
- **Concurrency:** parallel workers + a second concurrent session do not lose EMA updates (atomic SQL).
- **Real dogfood (qualitative):** run the same goal twice on cerberus; confirm the second run's plan references prior per-target outcomes.

---

## 10. Out of scope / follow-ups
- Per-attempt `recExecResult`-based attribution (tighter than case-level) — §4.2 acknowledges case-level attribution remains coarse (free-rider/scapegoat possible) after dedup+skip fixes.
- Semantic-memory dedup beyond "keep newest".
- Backfilling embeddings for procedural rows written before this change (`reembed` covers going forward; historical rows without embeddings are simply not recalled until reembedded).
- Migrating `GetProceduralByMatch` callers (CLI only) to embedding recall.

---

## 11. Implementation order (suggested)
1. Migration (§7) + `NormalizeTarget` (§5-A1).
2. L1 write in examiner (§6.1) — immediate standalone value.
3. L3 embedding storage at `Learn` + `GetProceduralByEmbedding` + recovery switch (§4.1).
4. `memory_usage` write in recovery + dedup (§4.2).
5. Atomic EMA + consolidate phase (§5-F, §6.4).
6. Governance (§6.5).
7. `cerberus memory` CLI incl. `reembed` (§6.6, §5-G).
8. Resume dedup (§5-E) + dead-code cleanup (§6.7).
9. Tests at each step; integration two-session test; real dogfood.

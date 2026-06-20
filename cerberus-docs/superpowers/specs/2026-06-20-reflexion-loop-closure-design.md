# Reflexion Learning Loop Closure — Design (v3)

**Date:** 2026-06-20
**Status:** Design (pre-implementation)
**Revision history:** v1 over-built on a false premise (re-implemented planning recall that already exists). v2 fixed two fatal gaps but an adversarial review found three more fatal gaps + several underspecified items. v3 resolves all of them.

---

## 1. Goal

Make subsequent `cerberus run` sessions genuinely benefit from earlier sessions: recovery recalls lessons that actually apply, their effectiveness evolves with real outcomes, the procedural store does not accumulate duplicate lessons, memory does not grow without bound, and its state is inspectable.

**Non-goal:** re-building planning-time recall — Scout already recalls L1 (episodic) and L2 (semantic) memory in both direct and ToT modes (see §3).

---

## 2. Verified current state

Confirmed against code by three independent review passes.

| Layer | Write path | Read path | Loop status |
|---|---|---|---|
| **L1 episodic** | `store.RecordEpisodic` (`internal/store/memory.go:18`) — **no caller** | `store.GetEpisodicByTarget` in Scout (`memory_helpers.go:33`) | **Broken** — recall always empty |
| **L2 semantic** | `learner.storeSemanticFromReflections` → `StoreSemantic(..., embedder.ModelName())` (`learner_storage.go:115`) | `querySemanticMemories` → `SearchSemanticForProject` (`memory_helpers.go:75`) | **Closed** (but does **not** filter by `embedding_model` today — see §5-G) |
| **L3 procedural** | `learner.Learn` → `StoreProceduralWithType` (`learner_run.go`) — plain INSERT, no dedup | `GetProceduralByMatch` in recovery (`recovery.go:71`) — substring predicate | **Closed but inert** (§4.1) and **grows unbounded via duplicates** (§4.3) |

Supporting facts:
- `FinalVerdict` carries `StepResult.TestCase.Target`, `StepResult.Duration`, status in `{pass, fail, skip, uncertain}` (`examiner/policy.go`, `examiner/types.go`).
- CLI `run`, `cerberus serve`, `cerberus mcp` enter through one shared `Session.Run()` (`lifecycle_run.go`). `--resume` uses a separate `Session.Resume()` (`lifecycle_resume.go`) which re-runs `Examine()` → `Learn()` on remaining cases.
- Reflection conditions are **HTTP-shaped patterns**, not prose: the reflection prompt (`internal/head/examiner/prompts.go`) asks for things like `"POST /api/v1/* returned 401"`, `"* returned 5??"`.
- The embedder is `internal/embed.TrigramProvider`, model name `trigram-v1` — **character-trigram hashing, local, no API call, effectively free**. Scout and Learner each construct their own `NewTrigramProvider` today (`scout.go:39`, `learner_factory.go:15`); the session package has none.
- `TestCase.ID` is an LLM-generated string (`"tc-001"` style) — **unique within a session, NOT globally**.
- The verdicts table (`migrations/V001__cerberus.sql`) has `session_id, trace_id, target` — **no `case_id` column**.
- `memory_procedural` has `archived` (V002). `memory_episodic`, `memory_semantic` do **not**. `memory_semantic` has `embedding_model` (V004); `memory_procedural` has **no** embedding column.
- Reflexion defaults: `EpisodicLimit=10`, `SemanticTopK=5`, `SemanticThreshold=0.3` (`scout.go:38`).
- Existing governance helpers: `AutoArchiveLowEffectiveness`, `ArchiveProcedural`, `MarkStaleProcedural`.

---

## 3. Why earlier drafts were wrong

- v1 assumed "planning reads nothing" and proposed a new `Recaller` + recall phase. **False** — Scout already recalls L1+L2 in both planning modes (`buildEpisodicContext`, `plan_phases.go:64`, `direct_planning.go:92`). Planning code stays unchanged (§6.8).
- v2 assumed "Learning dedups" without an enforcing constraint or implementation. **False** — `StoreProceduralWithType` is a plain INSERT with no unique key, so resume + repeated sessions duplicate procedural memories unbounded.

---

## 4. Fatal findings (must fix before any of this is useful)

### 4.1 L3 recall predicate is inert — fix with embedding recall
`GetProceduralByMatch` (`procedural_query.go`) client-side filters `strings.Contains(target, condition) || strings.Contains(condition, target)`. Conditions are HTTP patterns and targets are paths; overlap is near-zero, so recovery recalls ~nothing.

**Fix — embedding-based L3 recall:**
- Migration: add `embedding TEXT NOT NULL DEFAULT '[]'` and `embedding_model TEXT NOT NULL DEFAULT ''` to `memory_procedural` (§7).
- At `Learn` time, embed each reflection's **condition** with the shared embedder (§6.0) and store it.
- New `GetProceduralByEmbedding(ctx, queryEmbedding, project, topK, threshold, currentModel)`: load non-archived rows for the project whose `embedding_model = currentModel`, cosine-rank, keep ≥ threshold, re-rank by `effectiveness`, return top-K.
- Recovery embeds its target and calls `GetProceduralByEmbedding`. Because the embedder is trigram-hashing and conditions share path/status-code trigrams with targets, hit rate is non-trivial (a few descriptive-condition reflections will recall poorly — acceptable decay).

### 4.2 Effectiveness attribution — fix dedup key + skip exclusion
v2 credited all 5 recalled memories by whole-case verdict, multi-counting across recovery attempts and mis-attributing skips.

**Fix:**
- Dedup key is **`(session_id, case_id, procedural_id)`** — at most one EMA update per recalled memory per case per session, regardless of recovery attempts. (`case_id` alone is not unique across sessions — §2.)
- Skip verdicts produce **no** effectiveness update.
- Success signal: `success = (final verdict == pass)`.
- Per-attempt `recExecResult` attribution is tighter but needs deeper plumbing; deferred to §10. Dedup + skip-exclusion already remove the fatal pathologies.

### 4.3 Procedural store grows unbounded via duplicates — fix with upsert
`StoreProceduralWithType` is a plain INSERT; `memory_procedural` has no unique constraint. Every `Learn()` (including every `--resume`, which re-runs `Examine`→`Learn` on remaining cases) inserts fresh rows for the same lesson. After N sessions the store has N copies of each lesson at effectiveness 0.5, dominating recall and defeating §1's non-growth goal.

**Fix:**
- Migration adds `UNIQUE(project_name, condition, action)` (§7).
- Rewrite `StoreProceduralWithType` as an upsert: `INSERT ... ON CONFLICT(project_name, condition, action) DO UPDATE SET category=excluded.category, type=excluded.type RETURNING id`. The effectiveness, usage_count, embedding, and embedding_model columns are **preserved** on conflict (not reset — that would wipe the EMA and embeddings).
- Embed the condition only when the row is genuinely new. On upsert-update, keep the existing embedding (stale-embedding handled by `reembed`, §6.6).
- Test (§9): two `Learn()` calls with the same reflection → one row.

---

## 5. Risk findings (must address for correctness/safety)

| # | Risk | Fix |
|---|---|---|
| **A1** | Episodic read key mismatch: `queryEpisodicMemories` looks up by `endpoint.Path` (e.g. `/users/{id}`) while the write would use the case's actual target (e.g. `/api/users/123`). | Define `NormalizeTarget`: lowercase, strip query string, replace `/\d+/` and `/[0-9a-f]{8,}/` with `/{id}`, strip trailing slash; key on **path only** (the read side already drops the method). Apply at BOTH the episodic write and the read (`memory_helpers.go:22`, which currently stores `Method` but never uses it). |
| **C** | `Learn()` runs before `PersistFinalVerdicts` inside `Examine()`. v2 placed episodic write "in executeExaminerPhase" and consolidate "after it" — contradictory, since verdicts are produced and committed inside `Examine()`, invisible to the caller uncommitted. | Both the episodic write and consolidate live in ONE new `executeConsolidatePhase()` in `lifecycle_run.go`, invoked AFTER `executeExaminerPhase` (verdicts guaranteed committed). Consolidate re-queries committed verdicts by `session_id` rather than trusting in-memory state. (§6.4) |
| **D** | Deleting `memory_usage` after consolidate loses debuggability and risks double-EMA on a mid-consolidate crash. | `memory_usage` keeps `consolidated_at`; consolidate filters `WHERE consolidated_at IS NULL` and stamps it. Physical delete only via `cerberus memory prune --hard`. |
| **E** | Resume dedup: verdicts table has no `case_id` (only `target`), and `Resume()` re-runs `Examine()` → `Learn()` on remaining cases, risking double application of episodic/effectiveness/learnings. | No special target-filtering is needed — idempotency covers it: (a) effectiveness EMA is guarded by `consolidated_at` (a row is updated at most once; if the original session crashed before consolidate, resume processes the still-NULL rows — correct crash recovery, not a bug); (b) episodic write only emits for resume's own verdicts, which by construction are the newly-run cases; (c) `Learn()` duplicates are prevented by the upsert (§4.3). A `verdicts.case_id` column for finer-grained dedup is deferred to §10. |
| **F** | `UpdateProceduralEffectiveness` is read-modify-write, unsafe under `--parallel`/concurrent sessions. | Atomic SQL: `UPDATE memory_procedural SET effectiveness = effectiveness*0.7 + 0.3*?, usage_count = usage_count+1 WHERE id = ?` (α=0.3). Drop the Go-side read. |
| **G** | `SearchSemanticForProject` does **not** filter by `embedding_model` today (silent cross-model mixing). v2 only added the filter to L3, leaving L2 inconsistent. | Add the model filter to **both** L2 (`SearchSemanticForProject`) and L3 (`GetProceduralByEmbedding`) for consistency. Consequence: on embedder model change, recall is empty until `cerberus memory reembed` (§6.6) regenerates — an explicit, visible step rather than a silent brain-wipe. |

---

## 6. Design (v3)

### 6.0 Shared embedder
Create one `TrigramProvider` in the session layer (or a shared helper on `Session`), threaded into `ReActLoopConfig` (new `Embedder` field) and through to `Recovery`. This **same instance** is used to embed L3 conditions at `Learn` time and L3 targets at recovery time — model mismatch would break cosine, so they must share. (Local trigram hashing = effectively free, so no cost concern.)

### 6.1 Episodic write
`RecordEpisodic(sessionID, NormalizeTarget(target), status, verdict, durationMs)` per verdict, performed inside the consolidate phase (§6.4), reading in-memory `rp.verdicts`. Skip verdicts whose case never executed (no target). This makes `buildEpisodicContext` return real per-target history on the next run.

### 6.2 L3 embedding recall (§4.1) + upsert (§4.3)
Migration + condition-embedding storage at `Learn` (upsert) + `GetProceduralByEmbedding`; recovery switches to it.

### 6.3 Effectiveness feedback
- Recovery writes `memory_usage` (PK `(session_id, case_id, procedural_id)`, see §7) when it recalls a memory. The `UNIQUE` constraint enforces §4.2 dedup at insert time (`INSERT OR IGNORE`).
- Consolidate (§6.4): for each unconsumed `memory_usage` row, look up the case's committed verdict; if `skip`, mark `consolidated_at` and continue; else apply the atomic EMA (§5-F) with `success = verdict == pass`; stamp `consolidated_at`.

### 6.4 Consolidate phase (new, single phase)
`executeConsolidatePhase()` on `runPhase`, invoked in `lifecycle_run.go` after `executeExaminerPhase`, and in the resume path (idempotent — §5-E). Responsibilities, in order:
1. Episodic write from `rp.verdicts` (§6.1).
2. Effectiveness EMA from `memory_usage` joined to committed verdicts re-queried by `session_id` (§6.3, §5-C).
3. Archive stale memory (§6.5).
4. **No deletion.**

### 6.5 Governance
Reuse existing helpers where possible.
- **L3 procedural:** `AutoArchiveLowEffectiveness` — archive when `effectiveness < 0.3 AND usage_count >= 5 AND age > 30d`.
- **L2 semantic:** archive when `age > 90d`, or duplicate of a newer row where duplicate = identical `content` hash OR cosine > 0.95 (keep newest by `created_at`).
- **L1 episodic:** archive when `age > 30d`.
- Migration adds `archived INTEGER NOT NULL DEFAULT 0` to `memory_episodic` and `memory_semantic`; all read queries add `WHERE archived = 0`.

### 6.6 `cerberus memory` CLI
- `list [--type procedural|semantic|episodic] [--project P] [--all]` — effectiveness/usage/age; hides archived unless `--all`.
- `show --id N` (uses existing `GetSemanticByID` and procedural/episodic equivalents).
- `prune [--dry-run] [--hard]` — soft-archive by default; `--hard` physically deletes.
- `reembed` — regenerate embeddings for **`memory_procedural.condition`** and **`memory_semantic.content`** with the current embedder model, updating both `embedding` and `embedding_model`. (L1 has no embedding column.)

### 6.7 Dead code
- **Remove:** `MatchStrategies`, `FormatStrategiesForPrompt` (superseded).
- **Keep & wire:** `RecordEpisodic` (§6.1), `UpdateProceduralEffectiveness` (rewritten atomically, §5-F).
- **Keep:** `GetSemanticByID` (CLI `show`), `GetProceduralByMatch` (CLI compatibility; recovery no longer depends on it).

### 6.8 Planning code
**Substantively unchanged.** L1+L2 recall already wired. The only Scout-side touch is wrapping the episodic read key in `NormalizeTarget` (§5-A1, applied at `memory_helpers.go:22`). Once L1 is written, planning sees per-target history with no other Scout changes.

---

## 7. Data model changes (single new migration, atomic, reversible)

```sql
-- L3 embedding recall (§4.1) + dedup (§4.3)
ALTER TABLE memory_procedural ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_procedural ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_procedural_dedup ON memory_procedural(project_name, condition, action);

-- Archival for L1/L2 (§6.5)
ALTER TABLE memory_episodic  ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_semantic  ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

-- Effectiveness feedback (§6.3). case_id is session-local, so session_id is part of the key.
CREATE TABLE IF NOT EXISTS memory_usage (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  procedural_id   INTEGER NOT NULL,
  session_id      TEXT    NOT NULL,
  case_id         TEXT    NOT NULL,
  target          TEXT    NOT NULL,
  attempt         INTEGER,           -- debug only; not part of dedup key
  created_at      DATETIME NOT NULL,
  consolidated_at DATETIME,
  UNIQUE(session_id, case_id, procedural_id)
);
CREATE INDEX idx_memory_usage_unconsolidated ON memory_usage(procedural_id) WHERE consolidated_at IS NULL;
```

`StoreProceduralWithType` becomes an upsert on `(project_name, condition, action)` (§4.3), preserving effectiveness/usage/embedding on conflict.

---

## 8. Error handling
Memory is an enhancement, never a dependency. Any failure in embedding, recall, write, upsert, or consolidate logs a warning and degrades: planning proceeds with empty memory; consolidate skips the failing row; an upsert conflict is handled by `ON CONFLICT`. Runs never abort due to memory. Migrations are additive with safe defaults.

---

## 9. Testing
- **Unit:** `NormalizeTarget` symmetry + param templating; `GetProceduralByEmbedding` ranking (effectiveness × similarity, `embedding_model` filter, archived filter); atomic EMA SQL; consolidate idempotency (`consolidated_at`), skip-exclusion, `(session_id, case_id, procedural_id)` dedup; **upsert dedup** (two `Learn()` same reflection → one row, effectiveness/embedding preserved); archival thresholds per layer; L2 dedup rule; `Learn` condition-embedding stored; CLI list/prune/reembed (reembed updates both tables + `embedding_model`).
- **Integration (mock LLM/embedder):** two-session sequence — session 1 → episodic written, L3 condition embedded + upserted; session 2 → planning prompt contains session-1 per-target history, recovery recalls an L3 by embedding, consolidate updates its effectiveness, a low-effectiveness memory is archived. Plus a **resume test**: resume re-runs `Learn` on remaining cases → no duplicate procedural rows (upsert), episodic/effectiveness only for newly-completed targets.
- **Concurrency:** parallel workers + a second concurrent session do not lose EMA updates (atomic SQL).
- **Model drift:** after a model change, recall is empty until `reembed`; `reembed` restores it.
- **Real dogfood (qualitative):** same goal twice on cerberus; second run's plan references prior per-target outcomes.

---

## 10. Out of scope / follow-ups
- Per-attempt `recExecResult`-based attribution (tighter than case-level). Case-level attribution retains free-rider/scapegoat noise after dedup+skip fixes.
- Adding `case_id` to the verdicts table to enable finer-grained resume dedup (today only target-based).
- Semantic dedup beyond hash/exact-cosine.
- Backfilling embeddings for procedural rows written before this change (handled going forward; `reembed` covers historical rows).

---

## 11. Implementation order (suggested)
1. Migration (§7) + `NormalizeTarget` (§5-A1) + shared embedder in session layer (§6.0).
2. `StoreProceduralWithType` upsert + condition embedding at `Learn` (§4.3) — stops unbounded growth immediately.
3. L1 episodic write in the new consolidate phase (§6.1, §6.4) — standalone value.
4. `GetProceduralByEmbedding` + recovery switch + recovery embedder wiring (§4.1).
5. `memory_usage` write in recovery + dedup (§4.2) + atomic EMA + consolidate effectiveness step (§5-F, §6.3).
6. Model filter on L2 + L3 recall (§5-G) + governance (§6.5).
7. `cerberus memory` CLI incl. `reembed` (§6.6).
8. Resume target-based dedup (§5-E) + dead-code cleanup (§6.7).
9. Tests at each step; integration two-session + resume tests; real dogfood.

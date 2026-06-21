# Reflexion Learning Loop Closure — Design (v4)

**Date:** 2026-06-20
**Status:** Design (pre-implementation)
**Revision history:** v1 over-built on a false premise. v2/v3 fixed fatal gaps but each adversarial review found more. v4 resolves: per-row EMA over-application (new fatal), the unique-index migration landmine, condition-text instability, the insert-vs-update embedding limitation, and underspecified Recovery wiring (embedder + session_id). Reviewers concur the architecture is sound; remaining items are implementation correctness.

---

## 1. Goal

Make subsequent `cerberus run` sessions genuinely benefit from earlier sessions: recovery recalls lessons that actually apply, their effectiveness evolves with real outcomes (at the right rate), the procedural store does not accumulate duplicate lessons, memory does not grow without bound, and its state is inspectable.

**Non-goal:** re-building planning-time recall — Scout already recalls L1 (episodic) and L2 (semantic) memory in both planning modes (§3).

---

## 2. Verified current state

Confirmed against code across four review passes.

| Layer | Write | Read | Status |
|---|---|---|---|
| **L1 episodic** | `RecordEpisodic` (`store/memory.go:18`) — **no caller** | `GetEpisodicByTarget` in Scout (`memory_helpers.go:33`) | **Broken** |
| **L2 semantic** | `storeSemanticFromReflections` → `StoreSemantic(...,ModelName())` (`learner_storage.go:115`) | `querySemanticMemories` → `SearchSemanticForProject` (`memory_helpers.go:75`) — **no model filter today** | **Closed** (silent cross-model mixing) |
| **L3 procedural** | `Learn` → `StoreProceduralWithType` (`learner_run.go`) — plain INSERT, no dedup | `GetProceduralByMatch` in recovery (`recovery.go:71`) — substring, inert | **Inert + grows unbounded** |

Supporting facts:
- `FinalVerdict` has `StepResult.TestCase.Target`, `StepResult.Duration`, status in `{pass,fail,skip,uncertain}`.
- CLI/serve/mcp share `Session.Run()` (`lifecycle_run.go`); `--resume` uses `Session.Resume()` which re-runs `Examine()`→`Learn()` on remaining cases.
- Reflection conditions are HTTP-shaped patterns (`"POST /api/v1/* returned 401"`), but LLM-phrased and **not byte-stable across sessions**.
- Embedder is `internal/embed.TrigramProvider` (`trigram-v1`) — character-trigram hashing, **local, free**. Scout & Learner each build their own; session package has none.
- `TestCase.ID` is session-local (`tc-001` style; fallback `tc-%03d` by index in `direct_planning.go:115`). NOT global.
- Verdicts table has `session_id, trace_id, target` — **no `case_id`**.
- `memory_procedural` has `archived` (V002); `memory_episodic`/`memory_semantic` do not. `memory_semantic` has `embedding_model` (V004); `memory_procedural` has no embedding column. **`memory_procedural` already contains duplicate `(project,condition,action)` rows today** (from `Learn` re-runs and a broken `SeedStrategies` dedup at `seed.go:18-22`).
- `PersistFinalVerdicts` commits verdicts synchronously inside `Examine()` (`examiner.go:117`), skipping verdicts with `TraceID==0` (never-executed cases). `GetVerdicts(sessionID)` exists.
- Recovery (`recovery.go`) is called per-case with `*TestCase` (has `.ID`,`.Target`) but has **no session_id and no embedder** today; `sessionID` reaches `loop.ExecutePlan` (`run_phases_agent.go:44`) but is not stored on `ReActLoop`/`Recovery`.
- `AutoArchiveLowEffectiveness` (`procedural_archive.go`) archives on `effectiveness < threshold` only — no usage/age clause.

---

## 3. Why earlier drafts were wrong

- v1: "planning reads nothing" — false (L1+L2 already recalled in both modes). Planning stays unchanged.
- v2/v3: "Learn dedups" / "stops unbounded growth" — false without an enforcing key AND condition normalization (LLM rephrasing leaks).
- v3: per-row EMA — false; it over-applies within a session by recall count.

---

## 4. Fatal findings

### 4.1 L3 recall inert → embedding recall
`GetProceduralByMatch` substring predicate never matches. **Fix:** add `embedding`+`embedding_model` to `memory_procedural`; embed the (normalized) condition at `Learn`; new `GetProceduralByEmbedding(ctx, queryEmb, project, topK, threshold, currentModel)` cosine-ranks, filters by `embedding_model=currentModel` and `archived=0`, re-ranks by effectiveness; recovery embeds its target and uses it. Trigram overlap of HTTP-pattern conditions with path targets gives non-trivial hit rate.

### 4.2 Effectiveness attribution — grouped, skip-excluded
- Dedup key `(session_id, case_id, procedural_id)` (case_id is session-local). `memory_usage` UNIQUE on it + `INSERT OR IGNORE`.
- **One EMA update per `(session_id, procedural_id)` per consolidate pass** — NOT per memory_usage row. Signal = pass-fraction over that memory's recalled cases in the session: `signal = passes / (passes + fails)`; cases with `skip` verdict are excluded from numerator and denominator. If all of a memory's cases were `skip`, no update (mark `consolidated_at`).
- This fixes v3's per-row over-application (a memory recalled for 3 cases gets ONE update, not three).

### 4.3 Procedural dedup → normalize + upsert + always-embed
LLM rephrasing makes raw `condition` unstable across sessions. **Fix:**
- `NormalizeCondition`: lowercase, collapse whitespace, strip trailing punctuation/wildcard-only artifacts. Applied in `Learn` before storage and used for both the unique key and the embedding input.
- Migration adds `UNIQUE(project_name, condition, action)` (on the normalized condition).
- `StoreProceduralWithType` → upsert `ON CONFLICT(project_name, condition, action) DO UPDATE SET category=excluded.category, type=excluded.type` — **preserves** effectiveness, usage_count, embedding, embedding_model.
- **Always embed** the normalized condition (write it in both INSERT and DO UPDATE branches). SQLite RETURNING can't distinguish insert-vs-update, and trigram embedding is free, so the "embed only new rows" optimization is dropped.
- `SeedStrategies` (`seed.go:18-22`) dedup bug fixed (the `continue` must skip the outer insert) so re-running `cerberus init` stops duplicating seed rows.

### 4.4 Migration landmine → dedup before unique index
Existing DBs already contain duplicate `(project,condition,action)` triples. A blind `CREATE UNIQUE INDEX` would ERROR and roll back the whole migration, bricking the app. **Fix:** the migration deduplicates first (keep newest by `created_at`, tiebreak `usage_count DESC`), then creates the index. See §7.

---

## 5. Risk findings

| # | Risk | Fix |
|---|---|---|
| **A1** | Episodic read key (`endpoint.Path`, e.g. `/users/{id}`) vs write key (case target, e.g. `/api/users/123`). | `NormalizeTarget`: lowercase, strip query, replace `/\d+/` and `/[0-9a-f]{8,}/` with `/{id}`, strip trailing slash; key on path only. Applied at write AND at the read (`memory_helpers.go:22`). |
| **C** | `Learn()` precedes `PersistFinalVerdicts` inside `Examine()`. | Episodic write + consolidate are ONE new `executeConsolidatePhase()` in `lifecycle_run.go` AFTER `executeExaminerPhase` (verdicts committed). Consolidate re-queries committed verdicts via `GetVerdicts(session_id)`. Skip cases (no committed verdict, `TraceID==0`) are sourced from in-memory `rp.verdicts` for skip-exclusion. |
| **D** | Deleting `memory_usage` after consolidate risks double-EMA on crash and loses debuggability. | Keep `consolidated_at`; filter `WHERE consolidated_at IS NULL`; stamp after applying. Physical delete only via `cerberus memory prune --hard`. |
| **E** | Resume re-runs `Learn()`. | Safe by idempotency: effectiveness via `consolidated_at`, episodic only for resume's new verdicts, `Learn` via upsert. No target-filtering needed. (`verdicts.case_id` deferred to §10.) |
| **F** | `UpdateProceduralEffectiveness` read-modify-write unsafe under `--parallel`. | Atomic SQL, applied **once per `(session_id, procedural_id)`** with the §4.2 fraction as signal: `UPDATE memory_procedural SET effectiveness = effectiveness*0.7 + 0.3*?, usage_count = usage_count+? WHERE id = ?` (α=0.3; `usage_count` incremented by the number of recalled cases that contributed signal). Drop the Go-side read. |
| **G** | `SearchSemanticForProject` does not filter by `embedding_model` today (silent cross-model mixing). | Add the filter to **both** L2 and L3 (§5-G applies to L2 too), as a standalone fix done early in implementation (§11 step 1) — not deferred. Strict `embedding_model = currentModel`. Legacy/empty-model rows are invisible until `reembed`; `reembed` backfills them. |

---

## 6. Design (v4)

### 6.0 Shared embedder + session_id into Recovery
One `TrigramProvider` in the session layer, threaded via `ReActLoopConfig` (add **`Embedder`** AND **`SessionID`** fields) → stored on `ReActLoop` → passed to `NewRecovery`. Recovery needs BOTH: the embedder to embed the target for L3 recall, and session_id to write `memory_usage`. `sessionID` is already available at `loop.ExecutePlan` (`run_phases_agent.go:44`) — capture it onto the loop. The same embedder instance embeds L3 conditions (Learn) and L3 targets (recovery).

### 6.1 Episodic write (in consolidate phase)
`RecordEpisodic(sessionID, NormalizeTarget(target), status, verdict, durationMs)` per verdict in `rp.verdicts`, inside the consolidate phase. Skip verdicts without a target.

### 6.2 L3 embedding recall + upsert + normalize (§4.1, §4.3)
Migration + `NormalizeCondition` + always-embed at `Learn` + upsert + `GetProceduralByEmbedding`; recovery switches to it (with embedder + session_id from §6.0).

### 6.3 Effectiveness feedback (grouped)
- Recovery writes `memory_usage(procedural_id, session_id, case_id, target, attempt)` (`INSERT OR IGNORE`, UNIQUE dedup).
- Consolidate groups unconsumed rows by `(session_id, procedural_id)`; for each group: gather the cases' verdicts (committed via `GetVerdicts`, skips from in-memory); compute `signal = passes/(passes+fails)` (skips excluded); if no non-skip cases, mark all rows consolidated and continue; else fire ONE atomic EMA (§5-F) with `signal` and increment `usage_count` by the non-skip case count; stamp all rows `consolidated_at`.

### 6.4 Consolidate phase (new, single)
`executeConsolidatePhase()` in `lifecycle_run.go` after `executeExaminerPhase` (and in resume, idempotent). Order: (1) episodic write, (2) grouped effectiveness EMA, (3) archive stale memory, (4) no deletion.

### 6.5 Governance (rewrite, not reuse)
`AutoArchiveLowEffectiveness` is **rewritten** (today it only checks effectiveness) to: archive L3 when `effectiveness < 0.3 AND usage_count >= 5 AND age > 30d`, OR `age > 90d AND usage_count < 2` (rare-useless clause). L2: archive when `age > 90d` or duplicate (identical `content` hash or cosine > 0.95; keep newest). L1: archive when `age > 30d`. Migration adds `archived` to `memory_episodic`/`memory_semantic`; reads add `WHERE archived=0`. (The §4.2 grouping fix removes the premature-archive interaction v3 had with per-row EMA.)

### 6.6 `cerberus memory` CLI
`list [--type --project --all]`, `show --id N`, `prune [--dry-run --hard]` (soft-archive default; `--hard` deletes), `reembed` (regenerate `memory_procedural.condition` and `memory_semantic.content` embeddings with current model; update `embedding_model`; covers legacy empty-model rows).

### 6.7 Dead code
Remove `MatchStrategies`, `FormatStrategiesForPrompt`. Wire `RecordEpisodic` (§6.1). Replace `UpdateProceduralEffectiveness` body with the atomic SQL (its only caller is consolidate). Keep `GetSemanticByID` (CLI), `GetProceduralByMatch` (CLI compat).

### 6.8 Planning code
Substantively unchanged; only `NormalizeTarget` wrap at `memory_helpers.go:22`.

---

## 7. Data model (one new migration)

```sql
-- L3 embedding (§4.1) + dedup (§4.3)
ALTER TABLE memory_procedural ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_procedural ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';

-- §4.4 landmine: dedup BEFORE the unique index (existing DBs already have dups)
DELETE FROM memory_procedural WHERE id NOT IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY project_name, condition, action
      ORDER BY created_at DESC, usage_count DESC
    ) AS rn FROM memory_procedural
  ) WHERE rn = 1
);
CREATE UNIQUE INDEX idx_procedural_dedup ON memory_procedural(project_name, condition, action);

-- Archival (§6.5)
ALTER TABLE memory_episodic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_semantic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

-- Effectiveness feedback (§6.3). case_id is session-local → session_id in key.
CREATE TABLE IF NOT EXISTS memory_usage (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  procedural_id INTEGER NOT NULL,
  session_id TEXT NOT NULL,
  case_id TEXT NOT NULL,
  target TEXT NOT NULL,
  attempt INTEGER,
  created_at DATETIME NOT NULL,
  consolidated_at DATETIME,
  UNIQUE(session_id, case_id, procedural_id)
);
CREATE INDEX idx_memory_usage_unconsolidated ON memory_usage(procedural_id) WHERE consolidated_at IS NULL;
```
`StoreProceduralWithType` → upsert on `(project_name, condition, action)` (condition normalized pre-insert), always embedding. `SeedStrategies` dedup bug fixed separately (`seed.go`).

---

## 8. Error handling
Memory is enhancement, never a dependency. Failures (embed/recall/write/upsert/consolidate) log warnings and degrade; planning proceeds with empty memory; consolidate skips failing groups; upsert conflicts handled by `ON CONFLICT`. Runs never abort on memory. Migrations additive + the dedup DELETE makes the unique index safe.

---

## 9. Testing
- **Unit:** `NormalizeTarget`/`NormalizeCondition` symmetry; `GetProceduralByEmbedding` ranking (effectiveness × similarity, model filter, archived filter); **grouped EMA** (one update per memory per session regardless of recall count; pass-fraction signal; skip exclusion; all-skip → no update); atomic EMA SQL; consolidate idempotency (`consolidated_at`); upsert dedup (two `Learn()` same normalized reflection → one row; effectiveness/embedding preserved on conflict); `SeedStrategies` idempotency (two `cerberus init` → no dup); governance thresholds incl. rare-useless clause; L2 dedup; `reembed` (both tables + `embedding_model`, covers empty-model rows).
- **Integration (mock LLM/embedder):** two-session sequence — session 1 → episodic written, L3 condition normalized+embedded+upserted; session 2 → planning prompt contains session-1 per-target history, recovery recalls L3 by embedding, consolidate applies ONE grouped EMA, low-effectiveness memory archived. Resume test: re-run `Learn` → no dup; episodic/effectiveness only for new cases.
- **Concurrency:** parallel workers + concurrent session do not lose EMA (atomic SQL) and grouping is stable.
- **Migration:** run the V008 migration against a DB pre-seeded with duplicate procedural rows → succeeds, dups collapsed, index created.
- **Model drift:** after model change recall empty until `reembed`; restored after.
- **Real dogfood (qualitative):** same goal twice; second run references prior per-target outcomes.

---

## 10. Out of scope / follow-ups
- Per-attempt `recExecResult` attribution (tighter than session-grouped pass-fraction).
- `verdicts.case_id` column for finer resume dedup.
- Semantic dedup beyond hash/cosine.
- Auto-`reembed` on model change (currently manual CLI).
- ~~Case-level environmental attribution~~ — **resolved 2026-06-21** (`feat/case-level-environmental-attribution`). Originally a known limitation: cases entering recovery were judged on the recovery's *final* result, masking an original environmental failure (HTTP-0) when recovery ended on a non-HTTP action, mis-classifying it `assertion_failed`. Fix: `stepExecution.environmentalSeen` tracks whether *any* attempt hit an environmental failure (via the shared `types.IsEnvironmentalFailure` predicate); `finalizeResult` then surfaces a "target unreachable" error on the final `StepResult`, so the examiner's `checkUnreachable` classifies the whole case environmental and excludes it from the EMA. Dogfood-verified: `assertion_failed` 5→3, `unreachable` 4→5.

---


## 11. Implementation order
1. Migration (§7) + `NormalizeTarget`/`NormalizeCondition` + shared embedder & session_id into `ReActLoopConfig`/`ReActLoop`/`Recovery` (§6.0) + **L2 `embedding_model` filter (§5-G)** + fix `SeedStrategies`.
2. `StoreProceduralWithType` upsert + always-embed normalized condition at `Learn` (§4.3) — stops growth.
3. L1 episodic write in new consolidate phase (§6.1, §6.4) — standalone value.
4. `GetProceduralByEmbedding` + recovery switch (§4.1).
5. `memory_usage` write + grouped atomic EMA + consolidate effectiveness step (§4.2, §5-F, §6.3).
6. Governance rewrite (§6.5).
7. `cerberus memory` CLI incl. `reembed` (§6.6).
8. Resume idempotency verification (§5-E) + dead-code cleanup (§6.7).
9. Tests at each step; integration two-session + resume; migration-on-dup DB; real dogfood.

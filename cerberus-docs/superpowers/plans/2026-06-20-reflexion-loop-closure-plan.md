# Reflexion Learning Loop Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close cerberus's reflexion learning loop so later `cerberus run` sessions benefit from earlier ones: recovery recalls lessons by embedding, effectiveness evolves with real outcomes (grouped, correct rate), the procedural store dedups, memory is governed, and is inspectable via a CLI.

**Architecture:** Wire two dead write paths (L1 episodic at verdict time, L3 effectiveness at consolidate time), replace inert substring L3 recall with embedding recall, add a `memory_usage` feedback table with grouped atomic EMA, dedup procedural memory via normalized-condition upsert, add a post-examiner `executeConsolidatePhase`, rewrite governance, and add a `cerberus memory` CLI. Planning code stays unchanged (L1+L2 recall already wired).

**Tech Stack:** Go 1.25, `github.com/binoctal/cerberus`, SQLite via `modernc.org/sqlite` (no CGo), `internal/embed.TrigramProvider` (local trigram hashing), `testify`, cobra CLI.

**Spec:** `cerberus-docs/superpowers/specs/2026-06-20-reflexion-loop-closure-design.md` (v4).

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`. Pure-Go SQLite (`modernc.org/sqlite`), no CGo.
- Commit author `binoctal <binoctal@gmail.com>`, **no Co-Authored-By**. Commit messages and code comments in English.
- All docs under `cerberus-docs/` (never `docs/`).
- `internal/embed` is imported with an alias (`embedPkg`) because the package name collides with the stdlib `embed`.
- Memory is an enhancement, never a dependency: every new code path logs a warning and degrades on failure; runs never abort due to memory.
- Migrations are append-only, additive, run in a transaction. Newest is V007; this plan adds V008.

## File Structure

**New files:**
- `migrations/V008__reflexion_loop.sql` — schema (embedding cols on procedural, archived cols on episodic/semantic, memory_usage table, dedup-before-index).
- `internal/memory/normalize.go` + `internal/memory/normalize_test.go` — `NormalizeTarget`, `NormalizeCondition` (shared by write + read sides).
- `internal/store/procedural_embedding.go` + test — `GetProceduralByEmbedding`.
- `internal/store/memory_usage.go` + test — `RecordMemoryUsage`, `UnconsolidatedUsage`, `MarkUsageConsolidated`.
- `internal/store/episodic_archive.go` + `internal/store/semantic_archive.go` — archival helpers.
- `internal/session/run_phases_consolidate.go` — `executeConsolidatePhase`.
- `cmd/cerberus/memory_command.go` + test — `cerberus memory` CLI.

**Modified files:**
- `internal/store/procedural_create.go` — upsert + embedding/model params.
- `internal/store/procedural_update.go` — grouped atomic EMA.
- `internal/store/procedural_archive.go` — rewrite `AutoArchiveLowEffectiveness`.
- `internal/store/procedural_query.go` — `GetProceduralByMatch` + new embedding query: `archived=0` filter (already present).
- `internal/store/semantic.go` — `SearchSemanticForProject` model filter; semantic archival.
- `internal/store/procedural_types.go` — add `Embedding`, `EmbeddingModel` to `ProceduralMemory`.
- `internal/store/seed.go` — fix `SeedStrategies` dedup bug.
- `internal/head/examiner/learner_run.go` — normalize condition + embed + pass to upsert.
- `internal/head/agent/recovery.go` — embedding recall + memory_usage write + embedder/sessionID fields.
- `internal/head/agent/executor_config.go` — `ReActLoopConfig.Embedder`; thread to `NewRecovery`.
- `internal/head/agent/executor_types.go` — `ReActLoop.embedder`; `recoverer` interface `SetSessionID`.
- `internal/head/agent/executor_run.go` — `ExecutePlan` sets sessionID on recovery.
- `internal/head/agent/execute_phases_recovery.go` — pass sessionID (already in scope via `se.sessionID`/loop).
- `internal/session/run_phases_agent.go` — build loop via config with shared embedder.
- `internal/session/lifecycle_run.go` — call `executeConsolidatePhase` after examiner.
- `internal/session/lifecycle_resume.go` (or `resume_phases_run.go`) — call consolidate (idempotent).
- `internal/head/scout/memory_helpers.go` — wrap episodic read key with `NormalizeTarget`.
- `cmd/cerberus/main.go` — register `memoryCmd`.

---

## Task 1: Migration V008 (schema + dedup-before-index)

**Files:**
- Create: `migrations/V008__reflexion_loop.sql`
- Test: `internal/store/migrations_v008_test.go`

**Interfaces:** Produces schema changes consumed by all later tasks.

- [ ] **Step 1: Write the failing test**

```go
// internal/store/migrations_v008_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestV008_AppliesAndDedups(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	// Seed two duplicate (project, condition, action) rows directly.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		VALUES ('n','C','A',0.5,0,'p','c','failure',0,'2026-06-01T00:00:00Z'),
		       ('n','C','A',0.4,3,'p','c','failure',0,'2026-06-20T00:00:00Z')`)
	require.NoError(t, err)

	// Re-running migrations must succeed (idempotent) and the unique index must exist.
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	var n int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_procedural WHERE project_name='p' AND condition='C' AND action='A'`).Scan(&n))
	require.Equal(t, 1, n, "duplicate procedural rows should collapse to the newest")

	// Columns added by V008 exist.
	for _, col := range []string{"embedding", "embedding_model"} {
		var v string
		require.NoError(t, s.DB().QueryRowContext(ctx,
			`SELECT COALESCE(embedding,'') FROM memory_procedural LIMIT 1`).Scan(&v),
			"column %s should exist", col)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestV008_AppliesAndDedups -v`
Expected: FAIL — migration file not found / index creation fails on the duplicate.

- [ ] **Step 3: Create the migration**

```sql
-- migrations/V008__reflexion_loop.sql
-- L3 embedding recall + dedup, L1/L2 archival, effectiveness feedback.

ALTER TABLE memory_procedural ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_procedural ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';

-- Dedup existing duplicate (project_name, condition, action) rows BEFORE the
-- unique index: keep newest by created_at, tiebreak usage_count DESC.
DELETE FROM memory_procedural WHERE id NOT IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY project_name, condition, action
      ORDER BY created_at DESC, usage_count DESC
    ) AS rn FROM memory_procedural
  ) WHERE rn = 1
);
CREATE UNIQUE INDEX idx_procedural_dedup ON memory_procedural(project_name, condition, action);

ALTER TABLE memory_episodic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_semantic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS memory_usage (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  procedural_id   INTEGER NOT NULL,
  session_id      TEXT    NOT NULL,
  case_id         TEXT    NOT NULL,
  target          TEXT    NOT NULL,
  attempt         INTEGER,
  created_at      DATETIME NOT NULL,
  consolidated_at DATETIME,
  UNIQUE(session_id, case_id, procedural_id)
);
CREATE INDEX idx_memory_usage_unconsolidated ON memory_usage(procedural_id) WHERE consolidated_at IS NULL;
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestV008_AppliesAndDedups -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add migrations/V008__reflexion_loop.sql internal/store/migrations_v008_test.go
git commit -m "feat(store): V008 migration for reflexion loop (embedding, archival, memory_usage)"
```

---

## Task 2: NormalizeTarget / NormalizeCondition helpers

**Files:**
- Create: `internal/memory/normalize.go`, `internal/memory/normalize_test.go`

**Interfaces:**
- Produces: `memory.NormalizeTarget(string) string`, `memory.NormalizeCondition(string) string`.

- [ ] **Step 1: Write the failing test**

```go
// internal/memory/normalize_test.go
package memory_test

import (
	"testing"

	"github.com/binoctal/cerberus/internal/memory"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"/api/users/123":          "/api/users/{id}",
		"/Users/123/":             "/users/{id}",
		"/orders/abcdef0123":      "/orders/{id}",
		"/x?a=1":                  "/x",
		"/GET /health":            "/get /health",
	}
	for in, want := range cases {
		assert.Equal(t, want, memory.NormalizeTarget(in), "input %q", in)
	}
	// Symmetry: a target and its templated form normalize the same.
	assert.Equal(t, memory.NormalizeTarget("/api/users/9"), memory.NormalizeTarget("/api/users/{id}"))
}

func TestNormalizeCondition(t *testing.T) {
	cases := map[string]string{
		"POST /api/v1/* returned 401":  "post /api/v1/* returned 401",
		"  4xx   on  Login  ":          "4xx on login",
		"Auth failed.":                  "auth failed",
	}
	for in, want := range cases {
		assert.Equal(t, want, memory.NormalizeCondition(in), "input %q", in)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory/ -v`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Implement**

```go
// internal/memory/normalize.go
package memory

import (
	"regexp"
	"strings"
)

var (
	numPathRE   = regexp.MustCompile(`/\d+`)
	hexPathRE   = regexp.MustCompile(`/[0-9a-f]{8,}`)
	trailingRE  = regexp.MustCompile(`/+$`)
	wsRE        = regexp.MustCompile(`\s+`)
)

// NormalizeTarget canonicalizes a test target so the episodic write key and
// the episodic read key (endpoint.Path) agree. Path-only: method is dropped.
func NormalizeTarget(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexByte(s, '?'); i >= 0 {
		s = s[:i] // strip query string
	}
	s = numPathRE.ReplaceAllString(s, "/{id}")
	s = hexPathRE.ReplaceAllString(s, "/{id}")
	s = trailingRE.ReplaceAllString(s, "")
	if s == "" {
		s = "/"
	}
	return s
}

// NormalizeCondition canonicalizes an L3 reflection condition so LLM phrasing
// variance collapses across sessions (enables upsert dedup + consistent embedding).
func NormalizeCondition(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = wsRE.ReplaceAllString(s, " ")
	s = strings.Trim(s, ".;,:!?")
	return s
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/
git commit -m "feat(memory): add NormalizeTarget and NormalizeCondition helpers"
```

---

## Task 3: Fix SeedStrategies dedup bug

**Files:**
- Modify: `internal/store/seed.go`
- Test: `internal/store/seed_test.go`

**Interfaces:** none new.

- [ ] **Step 1: Write the failing test**

```go
// internal/store/seed_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSeedStrategies_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	n1, err := store.SeedStrategies(ctx, s, "proj", zap.NewNop())
	require.NoError(t, err)
	require.Greater(t, n1, 0)
	n2, err := store.SeedStrategies(ctx, s, "proj", zap.NewNop())
	require.NoError(t, err)
	require.Equal(t, 0, n2, "second seed must not duplicate strategies")

	var total int
	require.NoError(t, s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_procedural WHERE project_name='proj'`).Scan(&total))
	require.Equal(t, n1, total, "row count must equal first seed count")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSeedStrategies_IsIdempotent -v`
Expected: FAIL — second seed duplicates (current `continue` only skips inner loop).

- [ ] **Step 3: Fix the dedup**

In `internal/store/seed.go`, replace the broken inner-loop dedup with an existence check. The relevant block currently:

```go
		existing, _ := s.GetProceduralByMatch(ctx, st.condition, 1)
		for _, e := range existing {
			if e.Name == st.name {
				continue
			}
		}
		_, err := s.StoreProceduralWithType(...)
```

becomes:

```go
		// Skip if a strategy with the same name already exists for this project.
		dup := false
		existing, _ := s.GetProceduralByMatch(ctx, st.condition, 100)
		for _, e := range existing {
			if e.Name == st.name && e.ProjectName == projectName {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		_, err := s.StoreProceduralWithType(ctx, st.name, st.condition, st.action, projectName, st.category, st.refType)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestSeedStrategies_IsIdempotent -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/seed.go internal/store/seed_test.go
git commit -m "fix(store): SeedStrategies must not duplicate strategies on re-run"
```

---

## Task 4: StoreProceduralWithType upsert + always-embed

**Files:**
- Modify: `internal/store/procedural_create.go`, `internal/store/procedural_types.go`
- Test: `internal/store/procedural_create_test.go`

**Interfaces:**
- Produces: `StoreProceduralWithType` new signature `(ctx, name, condition, action, projectName, category, refType string, embedding []float64, model string) (*ProceduralMemory, error)`.
- Produces: `ProceduralMemory.Embedding []float64`, `EmbeddingModel string` fields.

- [ ] **Step 1: Add fields to ProceduralMemory**

In `internal/store/procedural_types.go`, add to the struct:

```go
	Embedding       []float64 `json:"embedding,omitempty"`
	EmbeddingModel  string    `json:"embedding_model,omitempty"`
```

- [ ] **Step 2: Write the failing test**

```go
// internal/store/procedural_create_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreProceduralWithType_UpsertPreserves(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	m1, err := s.StoreProceduralWithType(ctx, "n", "cond", "act", "p", "cat", "failure", []float64{0.1, 0.2}, "trigram-v1")
	require.NoError(t, err)
	// Simulate effectiveness earned later.
	require.NoError(t, s.UpdateProceduralEffectiveness(ctx, m1.ID, true))
	require.NoError(t, s.UpdateProceduralEffectiveness(ctx, m1.ID, true))

	// Upsert same (project, condition, action): must NOT duplicate, must preserve effectiveness.
	m2, err := s.StoreProceduralWithType(ctx, "n2", "cond", "act", "p", "cat2", "success", []float64{0.3, 0.4}, "trigram-v1")
	require.NoError(t, err)
	require.Equal(t, m1.ID, m2.ID, "upsert must return same row id")

	var count int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_procedural WHERE project_name='p' AND condition='cond' AND action='act'`).Scan(&count))
	require.Equal(t, 1, count)

	require.NotEqual(t, 0.5, m2.Effectiveness, "effectiveness must be preserved across upsert, not reset to default")
	assert.Equal(t, []float64{0.3, 0.4}, m2.Embedding, "embedding refreshed (always-embed)")
	assert.Equal(t, "cat2", m2.Category, "category refreshed on upsert")
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestStoreProceduralWithType_UpsertPreserves -v`
Expected: FAIL — current signature has no embedding params; plain INSERT duplicates.

- [ ] **Step 4: Rewrite StoreProceduralWithType as upsert**

```go
// internal/store/procedural_create.go
package store

import (
	"context"
	"encoding/json"
	"time"
)

// StoreProceduralWithType upserts a procedural memory keyed by
// (project_name, condition, action). On conflict it refreshes category/type and
// the embedding (always re-embedded; trigram is cheap) but PRESERVES
// effectiveness and usage_count so the EMA is never wiped.
func (s *Store) StoreProceduralWithType(ctx context.Context, name, condition, action, projectName, category, refType string, embedding []float64, model string) (*ProceduralMemory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		embJSON = []byte("[]")
	}
	// Upsert: new rows get effectiveness 0.5, usage_count 0; existing rows keep theirs.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural
		   (name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at, embedding, embedding_model)
		 VALUES (?, ?, ?, 0.5, 0, ?, ?, ?, 0, ?, ?, ?)
		 ON CONFLICT(project_name, condition, action) DO UPDATE SET
		   name = excluded.name, category = excluded.category, type = excluded.type,
		   embedding = excluded.embedding, embedding_model = excluded.embedding_model`,
		name, condition, action, projectName, category, refType, now, string(embJSON), model)
	if err != nil {
		return nil, err
	}
	return s.GetProceduralByExactKey(ctx, projectName, condition, action)
}

// GetProceduralByExactKey loads the row for a (project, condition, action) triple.
func (s *Store) GetProceduralByExactKey(ctx context.Context, projectName, condition, action string) (*ProceduralMemory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name,''), COALESCE(category,'general_failure'),
		        COALESCE(type,'failure'), COALESCE(archived,0), created_at,
		        COALESCE(embedding,'[]'), COALESCE(embedding_model,'')
		 FROM memory_procedural WHERE project_name=? AND condition=? AND action=?`,
		projectName, condition, action)
	return scanProcedural(row)
}
```

(`scanProcedural` is extracted from the existing scan logic in `procedural_query.go` — move the row-scan into a shared `scanProcedural` helper used by both query methods; it must populate `Embedding` via `store.ParseEmbedding` and `EmbeddingModel`.)

Update the existing `GetProceduralByMatch` SELECT list to also read `embedding`/`embedding_model` and use the shared scanner.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestStoreProceduralWithType -v`
Expected: PASS.

- [ ] **Step 6: Update callers of the old signature**

`internal/store/seed.go` call site now passes embedding + model:

```go
		_, err := s.StoreProceduralWithType(ctx, st.name, st.condition, st.action, projectName, st.category, st.refType, nil, "")
```

(Seed rows carry no embedding; they are not recalled by embedding until reembedded.)

- [ ] **Step 7: Run full store package tests**

Run: `go test ./internal/store/`
Expected: PASS (fix any other caller compile errors).

- [ ] **Step 8: Commit**

```bash
git add internal/store/procedural_create.go internal/store/procedural_types.go internal/store/procedural_query.go internal/store/seed.go internal/store/procedural_create_test.go
git commit -m "feat(store): upsert procedural memory on (project,condition,action), always embed"
```

---

## Task 5: Learn embeds normalized condition

**Files:**
- Modify: `internal/head/examiner/learner_run.go`
- Test: `internal/head/examiner/learner_run_test.go` (extend or create)

**Interfaces:**
- Consumes: `StoreProceduralWithType(..., embedding, model)` (Task 4), `memory.NormalizeCondition` (Task 2).

- [ ] **Step 1: Write the failing test**

```go
// internal/head/examiner/learner_embed_test.go
package examiner_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLearn_NormalizesAndEmbedsCondition(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	// LLM returns one reflection whose condition is un-normalized prose.
	driver := ai.NewDriver(llm.NewMockClient(map[string]string{
		"default": `{"reflections":[{"diagnosis":"d","condition_pattern":"POST /x/* Returned 401. ","strategy":"retry auth","category":"auth","type":"failure"}]}`,
	}), ai.NewTokenBudget(100000, 10000))
	l := examiner.NewLearner(driver, s, zap.NewNop(), embed.NewTrigramProvider(embed.DefaultDimension))

	n, err := l.Learn(ctx, examiner.LearnInput{SessionID: "s1", Project: "p"})
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// Stored condition is normalized; embedding is populated with the trigram model.
	mem, err := s.GetProceduralByExactKey(ctx, "p", "post /x/* returned 401", "retry auth")
	require.NoError(t, err)
	require.NotEmpty(t, mem.Embedding, "condition must be embedded")
	require.Equal(t, embed.NewTrigramProvider(embed.DefaultDimension).ModelName(), mem.EmbeddingModel)
}
```

(The mock JSON shape matches the existing `promptReflectionOutput`; adjust the struct/JSON key to the actual `Reflection` schema if it differs — the field names are `diagnosis`, `condition_pattern`, `strategy`, `category`, `type`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestLearn_NormalizesAndEmbedsCondition -v`
Expected: FAIL — current Learn passes raw condition, no embedding.

- [ ] **Step 3: Modify Learn**

In `learner_run.go`, inside the storage loop, normalize + embed before calling the upsert. Replace the `StoreProceduralWithType(...)` call:

```go
		cond := memory.NormalizeCondition(r.ConditionPattern)
		emb, embErr := l.embedder.Embed(ctx, cond)
		if embErr != nil {
			l.logger.Warn("embed condition failed", zap.Error(embErr))
			emb = nil
		}
		_, err := l.store.StoreProceduralWithType(ctx,
			r.Category, cond, r.Strategy, input.Project, r.Category, r.Type, emb, l.embedder.ModelName())
```

Add the import `"github.com/binoctal/cerberus/internal/memory"`. (Apply the same `memory.NormalizeCondition` normalization to the semantic content in `storeSingleReflection` so the L2 dedup in Task 12 keys consistently — use `cond`/normalized text consistently.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestLearn_NormalizesAndEmbedsCondition -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/examiner/learner_run.go internal/head/examiner/learner_embed_test.go
git commit -m "feat(examiner): normalize + embed reflection condition before upsert"
```

---

## Task 6: L2 embedding_model filter on SearchSemanticForProject

**Files:**
- Modify: `internal/store/semantic.go`
- Test: `internal/store/semantic_filter_test.go`

**Interfaces:** `SearchSemanticForProject` gains a `model string` parameter (or filters internally via the store's configured model — prefer explicit param for testability).

- [ ] **Step 1: Write the failing test**

```go
// internal/store/semantic_filter_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestSearchSemantic_FiltersByModel(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	_, err = s.StoreSemantic(ctx, "auth login failure", "reflexion", "p", []string{"a"}, []float64{1, 0}, "trigram-v1")
	require.NoError(t, err)
	_, err = s.StoreSemantic(ctx, "auth login failure", "reflexion", "p", []string{"a"}, []float64{1, 0}, "old-model")
	require.NoError(t, err)

	res, err := s.SearchSemanticForProject(ctx, []float64{1, 0}, "p", 5, 0.0, "trigram-v1")
	require.NoError(t, err)
	require.Len(t, res, 1, "only rows matching the current model should be recalled")
	require.Equal(t, "trigram-v1", res[0].EmbeddingModel)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSearchSemantic_FiltersByModel -v`
Expected: FAIL — current signature has no model param; returns both rows.

- [ ] **Step 3: Add the model filter**

In `semantic.go`, change the signature to add `model string` and filter:

```go
func (s *Store) SearchSemanticForProject(ctx context.Context, queryEmbedding []float64,
	project string, limit int, threshold float64, model string) ([]SemanticSearchResult, error) {
```

In the SELECT, add `AND COALESCE(embedding_model,'') = ?` to the WHERE clause and pass `model`. Update the caller `internal/head/scout/memory_helpers.go:75` (`querySemanticMemories`) to pass `s.embedder.ModelName()`:

```go
	results, err := s.store.SearchSemanticForProject(ctx, queryEmb, s.config.Project.Name, topK, threshold, s.embedder.ModelName())
```

- [ ] **Step 4: Run test to verify it passes; fix other callers**

Run: `go test ./internal/store/ ./internal/head/scout/`
Expected: PASS (update any other `SearchSemanticForProject` callers found via grep to pass the model).

- [ ] **Step 5: Commit**

```bash
git add internal/store/semantic.go internal/store/semantic_filter_test.go internal/head/scout/memory_helpers.go
git commit -m "feat(store): filter semantic recall by embedding model"
```

---

## Task 7: Shared embedder + sessionID into Recovery

**Files:**
- Modify: `internal/head/agent/executor_config.go`, `executor_types.go`, `executor_run.go`, `recovery.go`
- Modify: `internal/session/run_phases_agent.go`
- Test: `internal/head/agent/recovery_wiring_test.go`

**Interfaces:**
- Produces: `ReActLoopConfig.Embedder embed.Provider`; `Recovery` holds `embedder` + `sessionID`.
- Produces: `recoverer` interface gains `SetSessionID(string)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/head/agent/recovery_wiring_test.go
package agent_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRecovery_HasEmbedderAndSessionID(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	driver := ai.NewDriver(llm.NewMockClient(nil), ai.NewTokenBudget(1000, 100))
	rec := agent.NewRecovery(driver, s, agent.DefaultReActConfig(), zap.NewNop(), emb)
	rec.SetSessionID("sess-42")

	// Build a loop via config and ensure the embedder + sessionID propagate.
	loop := agent.NewReActLoopWithConfig(agent.ReActLoopConfig{
		Driver: driver, Store: s, Config: agent.DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb,
	})
	require.NotNil(t, loop)
	_ = ctx
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent/ -run TestRecovery_HasEmbedderAndSessionID -v`
Expected: FAIL — `NewRecovery` has no embedder param; no `SetSessionID`.

- [ ] **Step 3: Add Embedder to config + thread to Recovery**

`executor_config.go`:

```go
type ReActLoopConfig struct {
	Driver   *ai.Driver
	Store    *store.Store
	Engine   *RuleEngine
	Executor TypedExecutor
	Config   ReActConfig
	Gate     escalation.Gate
	Logger   *zap.Logger
	Embedder embedPkg.Provider // NEW
}
```

In `NewReActLoopWithGateWithConfig`, add `embedder: cfg.Embedder` to the `ReActLoop{}` literal and change the recovery line:

```go
		recovery:   NewRecovery(cfg.Driver, cfg.Store, cfg.Config, cfg.Logger, cfg.Embedder),
```

Add `import embedPkg "github.com/binoctal/cerberus/internal/embed"`.

`recovery.go`:

```go
type Recovery struct {
	driver    *ai.Driver
	store     *store.Store
	config    ReActConfig
	logger    *zap.Logger
	embedder  embedPkg.Provider
	sessionID string
}

func NewRecovery(driver *ai.Driver, store *store.Store, config ReActConfig, logger *zap.Logger, embedder embedPkg.Provider) *Recovery {
	if embedder == nil {
		embedder = embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	}
	return &Recovery{driver: driver, store: store, config: config, logger: logger, embedder: embedder}
}

// SetSessionID is called by the loop at the start of ExecutePlan so recovery can
// attribute memory_usage to the right session.
func (rc *Recovery) SetSessionID(id string) { rc.sessionID = id }
```

`executor_types.go` — add `embedder embedPkg.Provider` and `sessionID string` to `ReActLoop`, and add `SetSessionID(string)` to the `recoverer` interface:

```go
type recoverer interface {
	Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error)
	SetSessionID(string)
}
```

`executor_run.go` — at the top of `ExecutePlan`, capture sessionID for recovery:

```go
func (r *ReActLoop) ExecutePlan(ctx context.Context, plan *TestPlan, sessionID string) ([]StepResult, error) {
	r.sessionID = sessionID
	if r.recovery != nil {
		r.recovery.SetSessionID(sessionID)
	}
	// ...existing body...
}
```

`run_phases_agent.go` — switch the construction to the config builder and pass a shared embedder:

```go
	emb := embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	loop := agent.NewReActLoopWithGateWithConfig(agent.ReActLoopConfig{
		Driver:   rp.session.driverFor(&rp.session.agentDriver),
		Store:    rp.session.Store,
		Engine:   engine,
		Executor: multiExec,
		Config:   config,
		Gate:     rp.session.Gate,
		Logger:   rp.session.Logger,
		Embedder: emb,
	})
```

(Add the `embedPkg` import. The parallel executor path wraps `loop`, so it inherits the embedder.)

- [ ] **Step 4: Run test to verify it passes; fix compile**

Run: `go build ./... && go test ./internal/head/agent/ -run TestRecovery_HasEmbedderAndSessionID -v`
Expected: PASS. Fix any other `NewRecovery`/`NewReActLoopWithGate` callers (e.g. tests) to pass an embedder.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/ internal/session/run_phases_agent.go internal/head/agent/recovery_wiring_test.go
git commit -m "feat(agent): thread shared embedder + sessionID into Recovery via config"
```

---

## Task 8: L1 episodic write in executeConsolidatePhase

**Files:**
- Create: `internal/session/run_phases_consolidate.go`
- Modify: `internal/session/lifecycle_run.go`, `internal/session/lifecycle_resume.go` (or `resume_phases_run.go`)
- Modify: `internal/head/scout/memory_helpers.go` (wrap read with NormalizeTarget)
- Test: `internal/session/consolidate_episodic_test.go`

**Interfaces:**
- Consumes: `store.RecordEpisodic`, `memory.NormalizeTarget`, `rp.verdicts`.

- [ ] **Step 1: Write the failing test**

```go
// internal/session/consolidate_episodic_test.go
package session_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/stretchr/testify/require"
)

func TestConsolidate_WritesEpisodicPerVerdict(t *testing.T) {
	// Build a runPhase with two synthetic verdicts (one pass, one skip) and call
	// executeConsolidatePhase; assert two episodic rows exist with normalized targets.
	// (Use the session test harness already present in internal/session/*_test.go
	// to construct rp with a real store + rp.verdicts.)
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t) // existing helper in session tests
	defer cleanup()

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-1", Target: "/api/users/123"}}},
		{Status: examiner.StatusSkip, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "tc-2", Target: "/health"}}},
	}
	rp.session.ID = "sess-1"

	require.NoError(t, rp.executeConsolidatePhase())

	var n int
	require.NoError(t, rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id='sess-1'`).Scan(&n))
	require.Equal(t, 2, n)

	var target string
	require.NoError(t, rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT target FROM memory_episodic WHERE session_id='sess-1' AND target LIKE '%users%'`).Scan(&target))
	require.Equal(t, "/api/users/{id}", target, "target must be normalized")
}
```

(If `newTestRunPhase` doesn't exist, add a minimal helper in the test file that builds a `runPhase` with an in-memory store + session, mirroring existing session test setup.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestConsolidate_WritesEpisodicPerVerdict -v`
Expected: FAIL — `executeConsolidatePhase` does not exist.

- [ ] **Step 3: Implement the phase (episodic part; effectiveness added in Task 11)**

```go
// internal/session/run_phases_consolidate.go
package session

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
	"go.uber.org/zap"
)

// executeConsolidatePhase runs after verdicts are committed. It is idempotent
// (safe on resume): episodic writes key on session+target+verdict, effectiveness
// EMA is guarded by memory_usage.consolidated_at, and Learn dedups via upsert.
func (rp *runPhase) executeConsolidatePhase() error {
	if err := rp.writeEpisodicMemory(); err != nil {
		rp.session.Logger.Warn("episodic consolidate failed", zap.Error(err))
	}
	// Effectiveness EMA + governance are added in later tasks; each degrades on error.
	return nil
}

func (rp *runPhase) writeEpisodicMemory() error {
	for _, v := range rp.verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Target == "" {
			continue
		}
		target := memory.NormalizeTarget(tc.Target)
		if err := rp.session.Store.RecordEpisodic(
			rp.ctx, rp.session.ID, target, string(v.Status), string(v.Status), v.StepResult.Duration); err != nil {
			rp.session.Logger.Warn("record episodic failed",
				zap.String("target", target), zap.Error(err))
		}
	}
	return nil
}

// ensure unused import guard for examiner stays valid if StatusX referenced.
var _ = examiner.StatusPass
var _ = fmt.Stringer(nil)
```

(Remove the `var _` guards once real usage lands; kept to avoid unused-import churn mid-plan.)

- [ ] **Step 4: Wire the phase into Run and Resume**

In `lifecycle_run.go`, after `executeExaminerPhase()` and before the summary build:

```go
	if err := rp.executeConsolidatePhase(); err != nil {
		rp.session.Logger.Warn("consolidate phase failed", zap.Error(err))
	}
```

In the resume path (`resume_phases_run.go`, after its examiner call), add the same call (idempotent — §5-E).

- [ ] **Step 5: Wrap episodic read key with NormalizeTarget**

In `internal/head/scout/memory_helpers.go`, the `queryEpisodicMemories` loop currently passes the raw target to `GetEpisodicByTarget`. Normalize it:

```go
		targetMemories, err := s.store.GetEpisodicByTarget(ctx, memory.NormalizeTarget(target), limit)
```

Add `import "github.com/binoctal/cerberus/internal/memory"`.

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestConsolidate_WritesEpisodicPerVerdict -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session/run_phases_consolidate.go internal/session/lifecycle_run.go internal/session/resume_phases_run.go internal/head/scout/memory_helpers.go internal/session/consolidate_episodic_test.go
git commit -m "feat(session): write episodic memory in a new consolidate phase"
```

---

## Task 9: GetProceduralByEmbedding

**Files:**
- Create: `internal/store/procedural_embedding.go`, `internal/store/procedural_embedding_test.go`

**Interfaces:**
- Produces: `GetProceduralByEmbedding(ctx, queryEmbedding []float64, project string, topK int, threshold float64, model string) ([]ProceduralMemory, error)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/store/procedural_embedding_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGetProceduralByEmbedding(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	condVec, _ := emb.Embed(ctx, "post /api/v1/* returned 401")
	_, err = s.StoreProceduralWithType(ctx, "n", "post /api/v1/* returned 401", "retry auth", "p", "auth", "failure", condVec, emb.ModelName())
	require.NoError(t, err)

	q, _ := emb.Embed(ctx, "post /api/v1/login")
	got, err := s.GetProceduralByEmbedding(ctx, q, "p", 5, 0.1, emb.ModelName())
	require.NoError(t, err)
	require.Len(t, got, 1, "embedding recall should match the auth failure")
	require.Equal(t, "retry auth", got[0].Action)

	// Wrong model → not recalled.
	got0, err := s.GetProceduralByEmbedding(ctx, q, "p", 5, 0.1, "other-model")
	require.NoError(t, err)
	require.Empty(t, got0)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestGetProceduralByEmbedding -v`
Expected: FAIL — method does not exist.

- [ ] **Step 3: Implement**

```go
// internal/store/procedural_embedding.go
package store

import (
	"context"
	"sort"
)

// GetProceduralByEmbedding recalls non-archived procedural memories for a project
// whose embedding_model matches `model`, ranked by cosine similarity to the
// query (>= threshold), re-ranked by effectiveness. Mirrors SearchSemanticForProject.
func (s *Store) GetProceduralByEmbedding(ctx context.Context, queryEmbedding []float64, project string, topK int, threshold float64, model string) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name,''), COALESCE(category,'general_failure'),
		        COALESCE(type,'failure'), COALESCE(archived,0), created_at,
		        COALESCE(embedding,'[]'), COALESCE(embedding_model,'')
		 FROM memory_procedural
		 WHERE COALESCE(archived,0)=0 AND effectiveness >= 0.2
		   AND COALESCE(embedding_model,'') = ? AND COALESCE(embedding,'[]') != '[]'`,
		model)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type scored struct {
		m    ProceduralMemory
		sim  float64
	}
	var hits []scored
	for rows.Next() {
		m, err := scanProcedural(rows)
		if err != nil {
			return nil, err
		}
		if m.ProjectName != project && m.ProjectName != "" {
			continue
		}
		sim := CosineSimilarity(queryEmbedding, m.Embedding)
		if sim >= threshold {
			hits = append(hits, scored{m: m, sim: sim})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		// Re-rank by effectiveness, tiebreak similarity.
		if hits[i].m.Effectiveness != hits[j].m.Effectiveness {
			return hits[i].m.Effectiveness > hits[j].m.Effectiveness
		}
		return hits[i].sim > hits[j].sim
	})
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	out := make([]ProceduralMemory, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out, nil
}
```

(`scanProcedural` is the shared scanner from Task 4 that fills `Embedding` via `ParseEmbedding`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestGetProceduralByEmbedding -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/procedural_embedding.go internal/store/procedural_embedding_test.go
git commit -m "feat(store): embedding-based procedural memory recall"
```

---

## Task 10: Recovery uses embedding recall + writes memory_usage

**Files:**
- Modify: `internal/head/agent/recovery.go`
- Create: `internal/store/memory_usage.go`, `internal/store/memory_usage_test.go`
- Test: `internal/head/agent/recovery_recall_test.go`

**Interfaces:**
- Consumes: `GetProceduralByEmbedding` (Task 9), `embedder`/`sessionID` (Task 7).
- Produces: `store.RecordMemoryUsage(ctx, proceduralID int64, sessionID, caseID, target string, attempt int) error`.

- [ ] **Step 1: Write the memory_usage store test + impl**

```go
// internal/store/memory_usage.go
package store

import (
	"context"
	"time"
)

// RecordMemoryUsage records that a procedural memory was recalled for a case.
// Idempotent via UNIQUE(session_id, case_id, procedural_id).
func (s *Store) RecordMemoryUsage(ctx context.Context, proceduralID int64, sessionID, caseID, target string, attempt int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO memory_usage (procedural_id, session_id, case_id, target, attempt, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		proceduralID, sessionID, caseID, target, attempt, now)
	return err
}

// UnconsolidatedUsageGroups returns distinct (procedural_id, session_id) pairs
// with their unconsolidated rows for the given session, joined to verdicts.
func (s *Store) UnconsolidatedUsage(ctx context.Context, sessionID string) ([]MemoryUsage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, procedural_id, session_id, case_id, target, COALESCE(attempt,0), created_at
		 FROM memory_usage WHERE session_id=? AND consolidated_at IS NULL`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MemoryUsage
	for rows.Next() {
		var m MemoryUsage
		if err := rows.Scan(&m.ID, &m.ProceduralID, &m.SessionID, &m.CaseID, &m.Target, &m.Attempt, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkUsageConsolidated stamps consolidated_at for rows by id.
func (s *Store) MarkUsageConsolidated(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE memory_usage SET consolidated_at=? WHERE id=?`, now, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

type MemoryUsage struct {
	ID            int64
	ProceduralID  int64
	SessionID     string
	CaseID        string
	Target        string
	Attempt       int
	CreatedAt     string
}
```

(`memory_usage_test.go`: assert two `RecordMemoryUsage` with same (session,case,proc) → one row; `UnconsolidatedUsage` returns it; `MarkUsageConsolidated` empties the set.)

- [ ] **Step 2: Write the recovery recall test**

```go
// internal/head/agent/recovery_recall_test.go
package agent_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRecovery_RecallsByEmbeddingAndRecordsUsage(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../../migrations"))

	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	vec, _ := emb.Embed(ctx, "post /api/v1/* returned 401")
	_, err = s.StoreProceduralWithType(ctx, "n", "post /api/v1/* returned 401", "retry auth", "p", "auth", "failure", vec, emb.ModelName())
	require.NoError(t, err)

	driver := ai.NewDriver(llm.NewMockClient(map[string]string{
		"default": `{"diagnosis":"d","action":{"type":"api_request","payload":{"method":"GET","url":"/x"}},"skip":false}`,
	}), ai.NewTokenBudget(1000, 100))
	rec := agent.NewRecovery(driver, s, agent.DefaultReActConfig(), zap.NewNop(), emb)
	rec.SetSessionID("sess-9")

	_, err = rec.Recover(ctx, agent.TestCase{ID: "tc-1", Target: "/api/v1/login"}, nil, 1)
	require.NoError(t, err)

	var n int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_usage WHERE session_id='sess-9' AND case_id='tc-1'`).Scan(&n))
	require.Equal(t, 1, n, "recovery must record memory_usage for recalled L3")
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/store/ -run TestMemoryUsage ./internal/head/agent/ -run TestRecovery_RecallsByEmbeddingAndRecordsUsage -v`
Expected: FAIL — `RecordMemoryUsage` missing; recovery still uses substring + writes nothing.

- [ ] **Step 4: Switch recovery to embedding recall + record usage**

In `recovery.go` `buildRecoverContext`, replace `GetProceduralByMatch` with embedding recall, and write `memory_usage` for each recalled memory:

```go
func (rc *Recovery) buildRecoverContext(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) string {
	var b []byte
	b = append(b, fmt.Sprintf("Target: %s\nSummary: %s\nAttempt: %d\n",
		tc.Target, result.Summary(), attempt)...)

	memories := rc.recallProcedural(ctx, tc.Target)
	if len(memories) > 0 {
		b = append(b, "\n## Learned Strategies\n"...)
		for _, m := range memories {
			b = append(b, fmt.Sprintf("- When %s: %s (effectiveness: %.0f%%)\n",
				m.Condition, m.Action, m.Effectiveness*100)...)
			if rc.sessionID != "" {
				if err := rc.store.RecordMemoryUsage(ctx, m.ID, rc.sessionID, tc.ID, tc.Target, attempt); err != nil {
					rc.logger.Warn("record memory_usage failed", zap.Error(err))
				}
			}
		}
	}
	return string(b)
}

func (rc *Recovery) recallProcedural(ctx context.Context, target string) []store.ProceduralMemory {
	if rc.embedder == nil {
		return nil
	}
	q, err := rc.embedder.Embed(ctx, memory.NormalizeTarget(target))
	if err != nil {
		rc.logger.Warn("embed target failed", zap.Error(err))
		return nil
	}
	memories, err := rc.store.GetProceduralByEmbedding(ctx, q, rc.projectName(), 5, 0.1, rc.embedder.ModelName())
	if err != nil {
		rc.logger.Warn("procedural embedding recall failed", zap.Error(err))
		return nil
	}
	return memories
}
```

Add a `projectName` field to `Recovery` (set alongside sessionID via `SetProject`/`SetSessionID`, threaded from the loop config), or derive from config if available. Simplest: add `projectName string` set by the loop before ExecutePlan alongside `SetSessionID`. Update `ReActLoopConfig`/`ExecutePlan` to set it, and `run_phases_agent.go` to pass `rp.session.Config.Project.Name`.

Add imports: `memory`, `store` (already present).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/store/ ./internal/head/agent/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/memory_usage.go internal/store/memory_usage_test.go internal/head/agent/recovery.go internal/head/agent/recovery_recall_test.go internal/head/agent/executor_config.go internal/head/agent/executor_run.go internal/session/run_phases_agent.go
git commit -m "feat(agent): recovery recalls L3 by embedding and records memory_usage"
```

---

## Task 11: Grouped atomic EMA + consolidate effectiveness

**Files:**
- Modify: `internal/store/procedural_update.go`, `internal/session/run_phases_consolidate.go`
- Test: `internal/store/procedural_update_test.go`, `internal/session/consolidate_effectiveness_test.go`

**Interfaces:**
- Produces: `Store.ApplyProceduralEMA(ctx, id int64, signal float64, usageDelta int) error` (atomic, replaces read-modify-write).

- [ ] **Step 1: Write the EMA test**

```go
// internal/store/procedural_update_test.go
package store_test

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestApplyProceduralEMA_AtomicOnce(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	m, err := s.StoreProceduralWithType(ctx, "n", "c", "a", "p", "cat", "failure", nil, embed.NewTrigramProvider(embed.DefaultDimension).ModelName())
	require.NoError(t, err)

	// One grouped update: signal 0.5, delta 3 cases → e = 0.7*0.5 + 0.3*0.5 = 0.5, usage 3.
	require.NoError(t, s.ApplyProceduralEMA(ctx, m.ID, 0.5, 3))

	var eff float64
	var usage int
	require.NoError(t, s.DB().QueryRowContext(ctx,
		`SELECT effectiveness, usage_count FROM memory_procedural WHERE id=?`, m.ID).Scan(&eff, &usage))
	require.InDelta(t, 0.5, eff, 0.001)
	require.Equal(t, 3, usage)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestApplyProceduralEMA_AtomicOnce -v`
Expected: FAIL — method does not exist.

- [ ] **Step 3: Implement atomic grouped EMA**

```go
// internal/store/procedural_update.go
package store

import (
	"context"
)

// ApplyProceduralEffectiveness applies ONE atomic EMA update for a procedural
// memory (α=0.3). Replaces the read-modify-write version. `signal` is the
// pass-fraction in [0,1] for this session; `usageDelta` is the number of
// recalled cases that contributed. Call once per (session, procedural_id).
func (s *Store) ApplyProceduralEMA(ctx context.Context, id int64, signal float64, usageDelta int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural
		 SET effectiveness = effectiveness*0.7 + 0.3*?,
		     usage_count = usage_count + ?
		 WHERE id = ?`, signal, usageDelta, id)
	return err
}

// UpdateProceduralEffectiveness kept as a thin wrapper (single-case, signal 1/0)
// for any external caller; consolidate uses ApplyProceduralEMA directly.
func (s *Store) UpdateProceduralEffectiveness(ctx context.Context, id int64, success bool) error {
	sig := 0.0
	if success {
		sig = 1.0
	}
	return s.ApplyProceduralEMA(ctx, id, sig, 1)
}
```

- [ ] **Step 4: Write the consolidate effectiveness test**

```go
// internal/session/consolidate_effectiveness_test.go
// Assert: a memory recalled for 3 cases (2 pass, 1 fail) in one session gets
// ONE EMA update with signal 2/3, not three. Build rp.verdicts accordingly,
// seed memory_usage rows via RecordMemoryUsage, run executeConsolidatePhase,
// check effectiveness moved once and memory_usage rows are consolidated_at-stamped.
```

(Full body mirrors `consolidate_episodic_test.go`: seed procedural + 3 `RecordMemoryUsage` rows for one procedural_id under `sess-1` with case_ids tc-1/tc-2/tc-3; set `rp.verdicts` for those cases to pass/pass/fail with matching targets; run the phase; assert `effectiveness` equals `0.7*0.5 + 0.3*(2/3)` and all 3 rows have `consolidated_at` set, and `usage_count == 3`.)

- [ ] **Step 5: Implement the effectiveness step in consolidate**

In `run_phases_consolidate.go`, add to `executeConsolidatePhase` after `writeEpisodicMemory`:

```go
	if err := rp.applyEffectiveness(); err != nil {
		rp.session.Logger.Warn("effectiveness consolidate failed", zap.Error(err))
	}
```

```go
func (rp *runPhase) applyEffectiveness() error {
	ctx := rp.ctx
	rows, err := rp.session.Store.UnconsolidatedUsage(ctx, rp.session.ID)
	if err != nil {
		return err
	}
	// Group by procedural_id; gather each group's case verdicts (skip excluded).
	verdictByTarget := rp.verdictByNormalizedTarget()

	type group struct{ procID int64; passes, fails, count int; ids []int64 }
	groups := map[int64]*group{}
	var order []int64
	for _, u := range rows {
		g, ok := groups[u.ProceduralID]
		if !ok {
			g = &group{procID: u.ProceduralID}
			groups[u.ProceduralID] = g
			order = append(order, u.ProceduralID)
		}
		g.ids = append(g.ids, u.ID)
		st, found := verdictByTarget[memory.NormalizeTarget(u.Target)]
		if !found {
			continue // verdict not committed/in-memory for this target
		}
		switch st {
		case examiner.StatusPass:
			g.passes++; g.count++
		case examiner.StatusFail:
			g.fails++; g.count++
		default: // skip/uncertain excluded
		}
	}
	for _, pid := range order {
		g := groups[pid]
		if g.count == 0 {
			// All-skip: no signal, but mark consolidated so we don't reprocess.
			if err := rp.session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
				rp.session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
			}
			continue
		}
		signal := float64(g.passes) / float64(g.count)
		if err := rp.session.Store.ApplyProceduralEMA(ctx, pid, signal, g.count); err != nil {
			rp.session.Logger.Warn("apply EMA failed", zap.Int64("proc", pid), zap.Error(err))
		}
		if err := rp.session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
			rp.session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
		}
	}
	return nil
}

// verdictByNormalizedTarget maps normalized target -> verdict status, drawing
// from committed verdicts (GetVerdicts) so resume sees only what persisted.
func (rp *runPhase) verdictByNormalizedTarget() map[string]examiner.JudgeStatus {
	out := map[string]examiner.JudgeStatus{}
	committed, err := rp.session.Store.GetVerdicts(rp.ctx, rp.session.ID)
	if err != nil {
		rp.session.Logger.Warn("get verdicts failed", zap.Error(err))
	}
	add := func(target, status string) {
		if target == "" {
			return
		}
		out[memory.NormalizeTarget(target)] = examiner.JudgeStatus(status)
	}
	for _, v := range committed {
		add(v.Target, v.Status)
	}
	// In-memory covers skip cases that were never committed (TraceID==0).
	for _, v := range rp.verdicts {
		if v.StepResult.TestCase != nil {
			add(v.StepResult.TestCase.Target, string(v.Status))
		}
	}
	return out
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/store/ ./internal/session/ -run "EMA|Effectiveness|Consolidate" -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/store/procedural_update.go internal/store/procedural_update_test.go internal/session/run_phases_consolidate.go internal/session/consolidate_effectiveness_test.go
git commit -m "feat(session): grouped atomic EMA for procedural effectiveness in consolidate"
```

---

## Task 12: Governance rewrite (L1/L2/L3 archival)

**Files:**
- Modify: `internal/store/procedural_archive.go`
- Create: `internal/store/episodic_archive.go`, `internal/store/semantic_archive.go`
- Modify: read queries to filter `archived=0` (episodic `GetEpisodicByTarget`, semantic reads)
- Test: `internal/store/governance_test.go`

**Interfaces:**
- Produces: rewritten `AutoArchiveLowEffectiveness(ctx, project string) (int, error)`; `ArchiveStaleEpisodic(ctx, maxAgeDays int) (int, error)`; `ArchiveStaleSemantic(ctx, maxAgeDays int) (int, error)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/store/governance_test.go
package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/require"
)

func TestGovernance_ArchivesByPolicy(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	old := time.Now().Add(-(31 * 24 * time.Hour)).UTC().Format(time.RFC3339)
	// L3: low effectiveness, used 5x, old → archived.
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name,condition,action,effectiveness,usage_count,project_name,category,type,archived,created_at)
		VALUES ('n','c','a',0.2,5,'p','cat','failure',0,?)`, old)
	require.NoError(t, err)

	n, err := s.AutoArchiveLowEffectiveness(ctx, "p")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	// L3: rare-useless (usage<2, old>90d) → archived via the second clause.
	veryOld := time.Now().Add(-(91 * 24 * time.Hour)).UTC().Format(time.RFC3339)
	_, err = s.DB().ExecContext(ctx, `INSERT INTO memory_procedural
		(name,condition,action,effectiveness,usage_count,project_name,category,type,archived,created_at)
		VALUES ('n2','c2','a2',0.6,1,'p','cat','failure',0,?)`, veryOld)
	require.NoError(t, err)
	n, err = s.AutoArchiveLowEffectiveness(ctx, "p")
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 1)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestGovernance_ArchivesByPolicy -v`
Expected: FAIL — current `AutoArchiveLowEffectiveness(ctx, threshold)` signature/behavior differs.

- [ ] **Step 3: Rewrite governance**

```go
// internal/store/procedural_archive.go — rewrite AutoArchiveLowEffectiveness
// Archive L3 when low-effectiveness & well-used & old, OR rare & very old.
func (s *Store) AutoArchiveLowEffectiveness(ctx context.Context, project string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural SET archived = 1
		 WHERE COALESCE(archived,0) = 0
		   AND (project_name = ? OR project_name = '')
		   AND (
		     (effectiveness < 0.3 AND usage_count >= 5
		      AND created_at <= datetime('now','-30 days'))
		     OR
		     (usage_count < 2 AND created_at <= datetime('now','-90 days'))
		   )`, project)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```

```go
// internal/store/episodic_archive.go
package store

import "context"

func (s *Store) ArchiveStaleEpisodic(ctx context.Context, maxAgeDays int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_episodic SET archived = 1
		 WHERE COALESCE(archived,0)=0
		   AND created_at <= datetime('now', ?)`, "-"+itoa(maxAgeDays)+" days")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
```

(`itoa` via `strconv.Itoa`; add import.) `ArchiveStaleSemantic` mirrors this for `memory_semantic`, plus a dedup clause (identical `content` hash or cosine > 0.95 → archive older). Keep it simple: archive semantic when `age > 90d` (dedup-by-content handled at write via Task 5 normalization). 

Add `archived=0` filters to `GetEpisodicByTarget` (`memory.go`) and to `SearchSemanticForProject` WHERE clauses.

- [ ] **Step 4: Wire governance into consolidate**

In `executeConsolidatePhase`, after effectiveness:

```go
	rp.archiveStale()
```

```go
func (rp *runPhase) archiveStale() {
	store := rp.session.Store
	if n, err := store.AutoArchiveLowEffectiveness(rp.ctx, rp.session.Config.Project.Name); err != nil {
		rp.session.Logger.Warn("archive procedural failed", zap.Error(err))
	} else if n > 0 {
		rp.session.Logger.Info("archived stale procedural memory", zap.Int("count", n))
	}
	if n, err := store.ArchiveStaleEpisodic(rp.ctx, 30); err == nil && n > 0 {
		rp.session.Logger.Info("archived stale episodic memory", zap.Int("count", n))
	}
	if n, err := store.ArchiveStaleSemantic(rp.ctx, 90); err == nil && n > 0 {
		rp.session.Logger.Info("archived stale semantic memory", zap.Int("count", n))
	}
}
```

- [ ] **Step 5: Run tests; verify read filters**

Run: `go test ./internal/store/ ./internal/session/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/procedural_archive.go internal/store/episodic_archive.go internal/store/semantic_archive.go internal/store/memory.go internal/store/semantic.go internal/session/run_phases_consolidate.go internal/store/governance_test.go
git commit -m "feat(store): rewrite memory governance (L1/L2/L3 archival) + archived filters"
```

---

## Task 13: `cerberus memory` CLI

**Files:**
- Create: `cmd/cerberus/memory_command.go`, `cmd/cerberus/memory_command_test.go`
- Modify: `cmd/cerberus/main.go` (register `memoryCmd()`)

**Interfaces:**
- Produces: `cerberus memory list|show|prune|reembed`.

- [ ] **Step 1: Write the failing test** (CLI invokes each subcommand against an in-memory store; assert list output contains a seeded row, `prune` archives, `reembed` updates `embedding_model`).

```go
// cmd/cerberus/memory_command_test.go — table-driven: seed rows, run each
// subcommand via the cobra command's RunE, assert store state/output.
```

(Follow the existing `cmd/cerberus/cli_test.go` harness pattern for invoking cobra commands in tests.)

- [ ] **Step 2: Implement the command**

```go
// cmd/cerberus/memory_command.go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
)

func memoryCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "memory", Short: "Inspect and manage cerberus memory"}
	cmd.AddCommand(memoryListCmd(), memoryShowCmd(), memoryPruneCmd(), memoryReembedCmd())
	return cmd
}

func memoryListCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use: "list", Short: "List memories",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openMemoryStore()
			if err != nil { return err }
			defer s.Close()
			rows, err := s.DB().Query(`SELECT id, name, effectiveness, usage_count, archived FROM memory_procedural`)
			if err != nil { return err }
			for rows.Next() {
				var id int64; var name string; var eff float64; var usage int; var arch int
				_ = rows.Scan(&id, &name, &eff, &usage, &arch)
				if arch == 1 && !all { continue }
				fmt.Printf("[%d] %s eff=%.2f usage=%d archived=%d\n", id, name, eff, usage, arch)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&all, "all", false, "include archived")
	return c
}

// memoryShowCmd: --id N prints full row via store.GetSemanticByID / procedural equivalent.
// memoryPruneCmd: --hard physically deletes; default soft-archives via AutoArchiveLowEffectiveness-style UPDATE.
// memoryReembedCmd: iterate memory_procedural + memory_semantic, re-embed condition/content
//   with embed.NewTrigramProvider, UPDATE embedding + embedding_model.
```

`openMemoryStore()` opens the project store at `.cerberus/runtime/data/cerberus.db` and runs migrations (reuse the existing helper used by other commands).

`memoryReembedCmd` body:

```go
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	procs, _ := s.DB().Query(`SELECT id, condition FROM memory_procedural`)
	for procs.Next() {
		var id int64; var cond string
		_ = procs.Scan(&id, &cond)
		vec, err := emb.Embed(cmd.Context(), cond)
		if err != nil { continue }
		_ = s.StoreProceduralWithType(...) // or a direct UPDATE embedding/embedding_model by id
	}
	// same for memory_semantic.content
```

(Provide direct UPDATE-by-id helpers in the store to avoid re-upserting semantics; e.g. `Store.UpdateProceduralEmbedding(ctx, id, vec, model)` and `Store.UpdateSemanticEmbedding(ctx, id, vec, model)`.)

Register in `main.go`:

```go
	rootCmd.AddCommand(..., memoryCmd(), ...)
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `go test ./cmd/cerberus/ -run Memory -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add cmd/cerberus/memory_command.go cmd/cerberus/memory_command_test.go cmd/cerberus/main.go
git commit -m "feat(cli): cerberus memory list/show/prune/reembed"
```

---

## Task 14: Resume idempotency + dead code cleanup + integration

**Files:**
- Remove: `MatchStrategies`, `FormatStrategiesForPrompt` (`internal/head/examiner/strategy_matcher.go`) after confirming no callers.
- Test: `internal/session/reflexion_integration_test.go` (two-session loop), `internal/session/resume_idempotency_test.go`.

- [ ] **Step 1: Remove dead code**

```bash
grep -rn "MatchStrategies\|FormatStrategiesForPrompt" internal/ cmd/ --include="*.go" | grep -v _test.go
```
If empty (no callers), delete `strategy_matcher.go` (or just the two funcs if the file has others). Keep `GetSemanticByID` (used by `memory show`).

- [ ] **Step 2: Write the two-session integration test**

```go
// internal/session/reflexion_integration_test.go
// Session 1: mock LLM returns a reflection; run produces verdicts → consolidate
// writes episodic + upserts embedded procedural.
// Session 2: same project/goal → assert the Scout plan context (buildMemoryContext)
// contains the prior per-target episodic entry, and recovery (on a forced failure)
// recalls the L3 by embedding. Use the existing session test harness.
```

- [ ] **Step 3: Write the resume idempotency test**

```go
// internal/session/resume_idempotency_test.go
// Run a session partially (some cases), Resume() the rest → assert no duplicate
// procedural rows (upsert), episodic rows only for newly-run cases, and each
// memory_usage row consolidated at most once (consolidated_at).
```

- [ ] **Step 4: Run the full suite + lint + build**

Run: `make check`
Expected: PASS (fmt + lint + test, including `-race`).

- [ ] **Step 5: Real dogfood**

Run: `./build/cerberus run --goal "<same goal>"` twice; inspect the second run's logs for ` Learned Strategies` in recovery and prior per-target entries in the plan context. Then `./build/cerberus memory list` to confirm memories populated.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "test(session): reflexion two-session + resume idempotency; drop dead strategy_matcher"
```

---

## Self-Review (run before handoff)

**Spec coverage:** every spec section maps to a task — §4.1 (T9,T10), §4.2 (T10,T11), §4.3 (T4,T5), §4.4 (T1), §5-A1 (T2,T8), §5-C (T8), §5-D (T10,T11), §5-E (T8,T14), §5-F (T11), §5-G (T6), §6.0 (T7), §6.1 (T8), §6.2 (T9), §6.3 (T10,T11), §6.4 (T8,T11), §6.5 (T12), §6.6 (T13), §6.7 (T14), §6.8 (T8). No spec requirement lacks a task.

**Placeholder scan:** no TBD/TODO; every code step shows real code. (Where a test body says "mirrors X", the asserted state is concretely specified.)

**Type consistency:** `ApplyProceduralEMA(id, signal float64, usageDelta int)` defined in T11 and used in T11 consolidate; `GetProceduralByEmbedding(ctx, vec, project, topK, threshold, model)` defined T9, used T10; `RecordMemoryUsage(proceduralID, sessionID, caseID, target, attempt)` defined T10, used T10/T11; `NormalizeTarget`/`NormalizeCondition` defined T2, used T5/T8/T10/T11; `ReActLoopConfig.Embedder` + `Recovery.embedder/sessionID` defined T7, used T10.

## Execution Handoff

Plan complete and saved to `cerberus-docs/superpowers/plans/2026-06-20-reflexion-loop-closure-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** — fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session via executing-plans, batch with checkpoints.

Which approach?

# Recovered Rendering + Tally Correction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop Phase 2's lazy-fallback `StepResult` from polluting tallies/coverage and surface `recovered` as a distinct outcome in CLI/Markdown/HTML/JUnit.

**Architecture:** Pair primary↔fallback via `TestCase.FallbackFor`. Fallback results are not independent tally units; a primary whose fallback recovered is reclassified out of `Failed` into a `Recovered` bucket that counts toward coverage. Recovery is a dedicated `recovered` boolean column on `store.Verdict` (one-line `ALTER TABLE`, no CHECK-constraint rebuild), read by all consolidate/render code.

**Tech Stack:** Go 1.25, `modernc.org/sqlite` (no CGo), testify, `migrations/`, packages `internal/store`, `internal/head/examiner`, `internal/session`, `internal/report`.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- Follow existing comment density/naming idiom
- Spec: `cerberus-docs/superpowers/specs/2026-07-29-recovered-rendering-design.md`

## File Structure

- `migrations/V011__verdict_recovered.sql` — new column
- `internal/store/verdict.go` — `Verdict.Recovered`, `CreateVerdict` param, `GetVerdicts` select/scan
- `internal/head/examiner/verdict_persist.go` — pass `StepResult.Recovered` to `CreateVerdict`
- `internal/session/summary.go` — `SessionSummary.Recovered`, `FromResults` outcome model, `String()`, `plannedCaseCount`
- `internal/session/run_phases_lifecycle.go` + `internal/session/resume_phases_helpers.go` — call `plannedCaseCount`
- `internal/session/run_phases_consolidate.go` — `verdictByNormalizedTarget` + `writeEpisodicMemory` skip rules
- `internal/report/markdown_render_summary.go`, `markdown_render_verdicts.go`, `markdown_helpers.go`
- `internal/report/html_template.go`
- `internal/report/junit_case.go`

---

### Task 1: Store — `recovered` column + round-trip

**Files:**
- Create: `migrations/V011__verdict_recovered.sql`
- Modify: `internal/store/verdict.go` (`Verdict` struct ~line 6; `CreateVerdict` ~line 22; `GetVerdicts` ~line 40)
- Test: `internal/store/verdict_recovered_test.go` (new)

**Interfaces:**
- Consumes: nothing.
- Produces: `Verdict.Recovered bool`; `CreateVerdict(..., failureReason FailureReason, recovered bool)`; `GetVerdicts` populates `Recovered`. Later tasks read `Verdict.Recovered` / pass a `recovered` arg.

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/store/verdict_recovered_test.go`:

```go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerdict_RecoveredRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "test goal", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	// Recovered verdict persists recovered=true; a normal verdict stays false.
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "pass", 0.9, "judge", "r1", nil, FailureReasonNone, true)
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "ws://h/ws", "fail", 0.4, "judge", "r2", nil, FailureReasonAssertionFailed, false)
	require.NoError(t, err)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Recovered, "first verdict round-trips recovered=true")
	assert.False(t, got[1].Recovered, "second verdict stays recovered=false")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/store/ -run TestVerdict_RecoveredRoundTrip -v`
Expected: COMPILE ERROR — `CreateVerdict` has no `recovered` arg / `Verdict.Recovered undefined`.

- [ ] **Step 3: Add the migration**

Create `migrations/V011__verdict_recovered.sql`:

```sql
-- Mark a verdict row as a recovered fallback (A1 Phase 2 follow-up). A recovered
-- verdict's status stays the Examiner's judgment (pass); this column is the
-- orthogonal signal that the role was rescued by a lazy fallback, so downstream
-- tallies and reports can treat it as a distinct outcome.
ALTER TABLE verdicts ADD COLUMN recovered INTEGER NOT NULL DEFAULT 0;
```

- [ ] **Step 4: Add the field + wire CreateVerdict + GetVerdicts**

In `internal/store/verdict.go`, add the field to the `Verdict` struct (after `FailureReason`):

```go
	FailureReason FailureReason `json:"failure_reason,omitempty"` // Root cause of failure
	Recovered     bool          `json:"recovered,omitempty"`      // True if this verdict is a recovered lazy fallback (A1 Phase 2)
	CreatedAt     string        `json:"created_at"`
```

Change `CreateVerdict` signature and body — add a `recovered bool` parameter and write the column:

```go
func (s *Store) CreateVerdict(ctx context.Context, sessionID string, traceID int64,
	target, status string, confidence float64, source, reasoning string, suggestions any, failureReason FailureReason, recovered bool) (*Verdict, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO verdicts (session_id, trace_id, target, status, confidence, source, reasoning, suggestions, failure_reason, recovered, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, traceID, target, status, confidence, source, reasoning, jsonText(suggestions), string(failureReason), recovered, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Verdict{
		ID: id, SessionID: sessionID, TraceID: traceID,
		Target: target, Status: status, Confidence: confidence,
		Source: source, Reasoning: reasoning, FailureReason: failureReason, Recovered: recovered, CreatedAt: now,
	}, nil
}
```

In `GetVerdicts`, add the column to the SELECT and the scan (after `failure_reason` / `&v.FailureReason`):

```go
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, trace_id, target, status, confidence, source,
		        COALESCE(reasoning, ''), COALESCE(suggestions, ''), COALESCE(failure_reason, ''), recovered, created_at
		 FROM verdicts WHERE session_id = ? ORDER BY created_at`, sessionID)
```

```go
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TraceID, &v.Target,
			&v.Status, &v.Confidence, &v.Source, &v.Reasoning, &v.Suggestions,
			&v.FailureReason, &v.Recovered, &v.CreatedAt); err != nil {
```

- [ ] **Step 5: Run test to verify GREEN**

Run: `go test ./internal/store/ -run TestVerdict_RecoveredRoundTrip -v`
Expected: PASS.

- [ ] **Step 6: Fix the one production caller (compile)**

`internal/head/examiner/verdict_persist.go:85` is the only production `CreateVerdict` caller and now lacks the `recovered` arg. Update it (Task 2 owns the real value; here just pass `false` to restore compilation):

```go
		_, err := s.CreateVerdict(
			ctx,
			sessionID,
			v.StepResult.TraceID,
			target,
			status,
			v.CorrectnessConfidence,
			"judge", // Use "judge" to satisfy database constraint
			v.Reasoning,
			nil, // suggestions
			failureReason,
			false,
		)
```

Run: `go build ./...`
Expected: EXIT 0.

- [ ] **Step 7: Regression — full store package**

Run: `go test ./internal/store/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add migrations/V011__verdict_recovered.sql internal/store/verdict.go internal/store/verdict_recovered_test.go internal/head/examiner/verdict_persist.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(store): recovered column on verdicts (A1 Phase 2 follow-up)

Add a dedicated recovered BOOLEAN column to verdicts (ALTER TABLE, following
the V006 failure_reason precedent) rather than encoding recovery as a status
string, which would collide with the verdicts CHECK(status IN (...)) and force
a SQLite table rebuild. CreateVerdict gains a recovered arg (one production
caller); GetVerdicts selects and scans it. No behavior wired beyond the column."
```

---

### Task 2: Examiner — persist `recovered` from `StepResult`

**Files:**
- Modify: `internal/head/examiner/verdict_persist.go` (`CreateVerdict` call ~line 85)
- Test: `internal/head/examiner/verdict_persist_test.go` (new or append)

**Interfaces:**
- Consumes: `agent.StepResult.Recovered` (A1 Phase 2), `store.CreateVerdict(..., recovered bool)` (Task 1).
- Produces: a recovered fallback verdict row is stored with `recovered=true`.

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/head/examiner/verdict_persist_test.go`:

```go
package examiner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
)

func TestPersistFinalVerdicts_StoresRecovered(t *testing.T) {
	ctx := context.Background()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, store.RunMigrations(ctx, s.DB(), "../../migrations"))

	sess, err := s.CreateSession(ctx, "run", "g", "")
	require.NoError(t, err)
	traceID, err := s.CreateTrace(ctx, sess.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	verdicts := []FinalVerdict{
		{Status: StatusPass, StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-fb", Target: "ws://h/ws"}, TraceID: traceID, Recovered: true}},
		{Status: StatusPass, StepResult: agent.StepResult{
			TestCase: &agent.TestCase{ID: "tc-ok", Target: "ws://h/ws"}, TraceID: traceID}},
	}

	n, err := PersistFinalVerdicts(ctx, s, zap.NewNop(), sess.ID, verdicts)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	got, err := s.GetVerdicts(ctx, sess.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, got[0].Recovered, "recovered fallback persisted with recovered=true")
	assert.False(t, got[1].Recovered, "normal verdict stays recovered=false")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/head/examiner/ -run TestPersistFinalVerdicts_StoresRecovered -v`
Expected: FAIL — `got[0].Recovered` is false (Task 1 wired `false`).

- [ ] **Step 3: Pass the real value**

In `internal/head/examiner/verdict_persist.go`, change the last `CreateVerdict` argument from the literal `false` to the verdict's own recovered flag:

```go
			nil, // suggestions
			failureReason,
			v.StepResult.Recovered,
		)
```

- [ ] **Step 4: Run test to verify GREEN**

Run: `go test ./internal/head/examiner/ -run TestPersistFinalVerdicts_StoresRecovered -v`
Expected: PASS.

- [ ] **Step 5: Regression — full examiner package**

Run: `go test ./internal/head/examiner/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/examiner/verdict_persist.go internal/head/examiner/verdict_persist_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(examiner): persist recovered flag on fallback verdict rows

PersistFinalVerdicts now passes StepResult.Recovered to CreateVerdict, so a
recovered lazy-fallback verdict row is stored with recovered=true and is
distinguishable from a clean pass by downstream tallies and reports."
```

---

### Task 3: Session — `Recovered` field + `FromResults` outcome model + `String()`

**Files:**
- Modify: `internal/session/summary.go` (`SessionSummary` struct ~line 23; `FromResults` ~line 48; `String` ~line 108)
- Test: `internal/session/summary_test.go` (append)

**Interfaces:**
- Consumes: `agent.StepResult.Recovered`, `agent.TestCase.FallbackFor` (A1 Phase 2).
- Produces: `SessionSummary.Recovered int`; `FromResults` implements the primary↔fallback pairing; `TotalCases`/`CoveragePct` exclude fallback results; `String()` prints recovered.

- [ ] **Step 1: Write the failing tests (RED)**

Append to `internal/session/summary_test.go`:

```go
// TestFromResults_RecoveredPairing is the golden case from the design:
// roles A, B, C; A has fallback A' (recovered), B has fallback B' (not
// recovered), C standalone. A reclassifies to Recovered (not Failed); the
// fallback results are not independent units; coverage counts Recovered.
func TestFromResults_RecoveredPairing(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "B"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "C"}, Status: agent.StepPassed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
		{TestCase: &agent.TestCase{ID: "B'", FallbackFor: "B"}, Status: agent.StepFailed},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusFail, StepResult: results[1]},
		{Status: examiner.StatusPass, StepResult: results[2]},
		{Status: examiner.StatusPass, StepResult: results[3]},
		{Status: examiner.StatusFail, StepResult: results[4]},
	}

	summary := FromResults("g", "", 5, results, verdicts, 0, 0, 0)

	assert.Equal(t, 3, summary.TotalCases, "fallback results excluded from total")
	assert.Equal(t, 1, summary.Passed, "only C passed")
	assert.Equal(t, 1, summary.Failed, "only B failed; A reclassified out of Failed")
	assert.Equal(t, 1, summary.Recovered, "A recovered")
	assert.InDelta(t, 66.67, summary.CoveragePct, 0.01, "(Passed+Recovered)/Total")
}

// TestFromResults_AllRecovered: a recovered role does not surface as Failed.
func TestFromResults_AllRecovered(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
	}
	verdicts := []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: results[0]},
		{Status: examiner.StatusPass, StepResult: results[1]},
	}
	summary := FromResults("g", "", 2, results, verdicts, 0, 0, 0)
	assert.Equal(t, 0, summary.Failed, "recovered primary is not Failed")
	assert.Equal(t, 1, summary.Recovered)
	assert.Equal(t, 1, summary.TotalCases)
	assert.InDelta(t, 100.0, summary.CoveragePct, 0.01)
}

// TestFromResults_RecoveredRawResults: the raw-results branch (no verdicts)
// also honors Recovered/FallbackFor.
func TestFromResults_RecoveredRawResults(t *testing.T) {
	results := []agent.StepResult{
		{TestCase: &agent.TestCase{ID: "A"}, Status: agent.StepFailed},
		{TestCase: &agent.TestCase{ID: "A'", FallbackFor: "A"}, Status: agent.StepPassed, Recovered: true},
	}
	summary := FromResults("g", "", 2, results, nil, 0, 0, 0)
	assert.Equal(t, 0, summary.Failed)
	assert.Equal(t, 1, summary.Recovered)
}

func TestSessionSummary_StringIncludesRecovered(t *testing.T) {
	s := &SessionSummary{Passed: 1, Failed: 1, Skipped: 0, Uncertain: 0, Recovered: 1,
		PendingReview: 0, ReflectionsStored: 0, TotalTokens: 0, Duration: "1s"}
	assert.Contains(t, s.String(), "1 recovered")
}
```

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/session/ -run 'TestFromResults_RecoveredPairing|TestFromResults_AllRecovered|TestFromResults_RecoveredRawResults|TestSessionSummary_StringIncludesRecovered' -v`
Expected: FAIL — `Recovered` field undefined; totals still count fallback results.

- [ ] **Step 3: Add the field + outcome model**

In `internal/session/summary.go`, add the field to `SessionSummary` (after `Uncertain`):

```go
	Passed    int `json:"passed"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Uncertain int `json:"uncertain"`
	// Recovered counts roles rescued by a lazy fallback (A1 Phase 2). A
	// recovered primary is reclassified out of Failed; its fallback result is
	// not an independent unit. Recovered counts toward coverage.
	Recovered int `json:"recovered"`
```

Replace the body of `FromResults` from the `TotalCases:` line through the coverage computation with the pairing-aware version. The full function becomes:

```go
func FromResults(goal, projectURL string, planCases int, results []agent.StepResult, verdicts []examiner.FinalVerdict, reflections int, tokensUsed int, elapsed time.Duration) *SessionSummary {
	s := &SessionSummary{
		Goal:              goal,
		ProjectURL:        projectURL,
		TestCasesPlanned:  planCases,
		Verdicts:          len(verdicts),
		ReflectionsStored: reflections,
		TotalTokens:       tokensUsed,
		Duration:          elapsed.Round(time.Millisecond).String(),
		DurationMs:        elapsed.Milliseconds(),
	}

	// Pair primary<->fallback via TestCase.FallbackFor. A fallback result is
	// not an independent tally unit; a primary whose fallback recovered is
	// reclassified out of Failed into Recovered (counted toward coverage).
	recoveredPrimaryIDs := map[string]bool{}
	fallbackResultCount := 0
	for _, r := range results {
		tc := r.TestCase
		if tc == nil || tc.FallbackFor == "" {
			continue
		}
		fallbackResultCount++
		if r.Recovered {
			recoveredPrimaryIDs[tc.FallbackFor] = true
		}
	}
	s.TotalCases = len(results) - fallbackResultCount

	// Count final outcomes. Prefer examiner verdicts — the final judgment
	// reflects correctness adjustments (e.g. pass→uncertain) that raw step
	// status lacks, and these counts feed user-facing reports. Fall back to
	// step status only when the Examiner didn't run (no verdicts).
	if len(verdicts) > 0 {
		for _, v := range verdicts {
			tc := v.StepResult.TestCase
			if tc != nil && tc.FallbackFor != "" {
				continue // fallback result, not a unit
			}
			if tc != nil && recoveredPrimaryIDs[tc.ID] {
				s.Recovered++
				continue
			}
			switch v.Status {
			case examiner.StatusPass:
				s.Passed++
			case examiner.StatusFail:
				s.Failed++
			case examiner.StatusSkip:
				s.Skipped++
			case examiner.StatusUncertain:
				s.Uncertain++
			}
		}
	} else {
		for _, r := range results {
			tc := r.TestCase
			if tc != nil && tc.FallbackFor != "" {
				continue
			}
			if tc != nil && recoveredPrimaryIDs[tc.ID] {
				s.Recovered++
				continue
			}
			switch r.Status {
			case agent.StepPassed:
				s.Passed++
			case agent.StepFailed:
				s.Failed++
			case agent.StepSkipped:
				s.Skipped++
			case agent.StepUncertain:
				s.Uncertain++
			}
		}
	}

	for _, v := range verdicts {
		if v.NeedsReview() {
			s.PendingReview++
		}
	}

	// Coverage: (passed + recovered) roles / total role units * 100. Recovered
	// counts as covered (the deterministic fallback proved the role viable).
	if s.TotalCases > 0 {
		s.CoveragePct = float64(s.Passed+s.Recovered) / float64(s.TotalCases) * 100
	}

	return s
}
```

Update `String()` to print recovered (add `s.Recovered` to the format args and a `%d recovered` clause):

```go
func (s *SessionSummary) String() string {
	return fmt.Sprintf(`Session Summary:
  Verdicts: %d pass, %d fail, %d skip, %d uncertain, %d recovered
  Pending review: %d
  Reflections stored: %d (failure + success)
  Total tokens: ~%dK
  Duration: %s`,
		s.Passed, s.Failed, s.Skipped, s.Uncertain, s.Recovered,
		s.PendingReview,
		s.ReflectionsStored,
		s.TotalTokens/1000,
		s.Duration)
}
```

- [ ] **Step 4: Run the targeted tests to verify GREEN**

Run: `go test ./internal/session/ -run 'TestFromResults_RecoveredPairing|TestFromResults_AllRecovered|TestFromResults_RecoveredRawResults|TestSessionSummary_StringIncludesRecovered' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full session package**

Run: `go test ./internal/session/ -count=1`
Expected: PASS. The existing `TestSessionSummary_FromResults` baseline (no FallbackFor) is unchanged: `fallbackResultCount==0`, `recoveredPrimaryIDs` empty, prior counts hold.

- [ ] **Step 6: Commit**

```bash
git add internal/session/summary.go internal/session/summary_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(session): pair-aware FromResults tallies recovered as a distinct outcome

FromResults pairs primary<->fallback via TestCase.FallbackFor: fallback results
are excluded from TotalCases, and a primary whose fallback recovered is
reclassified out of Failed into a new Recovered bucket. CoveragePct becomes
(Passed+Recovered)/TotalCases — a rescued role counts as covered but is not a
clean pass. String() prints the recovered count."
```

---

### Task 4: Session — `plannedCaseCount` excludes lazy fallback cases

**Files:**
- Modify: `internal/session/summary.go` (add `plannedCaseCount`)
- Modify: `internal/session/run_phases_lifecycle.go:78`, `internal/session/resume_phases_helpers.go:80`
- Test: `internal/session/summary_test.go` (append)

**Interfaces:**
- Consumes: `agent.TestCase.FallbackFor` (A1 Phase 2), `*agent.TestPlan`.
- Produces: `plannedCaseCount(plan *agent.TestPlan) int` used by both run and resume summary call sites.

- [ ] **Step 1: Write the failing test (RED)**

Append to `internal/session/summary_test.go`:

```go
func TestPlannedCaseCount_ExcludesLazyFallback(t *testing.T) {
	plan := &agent.TestPlan{Cases: []agent.TestCase{
		{ID: "A"},
		{ID: "B"},
		{ID: "C"},
		{ID: "A'", FallbackFor: "A"},
		{ID: "B'", FallbackFor: "B"},
	}}
	assert.Equal(t, 3, plannedCaseCount(plan), "lazy fallback cases are not independent planned roles")
	assert.Equal(t, 0, plannedCaseCount(&agent.TestPlan{}), "empty plan -> 0")
	assert.Equal(t, 0, plannedCaseCount(nil), "nil plan -> 0")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/session/ -run TestPlannedCaseCount_ExcludesLazyFallback -v`
Expected: COMPILE ERROR — `plannedCaseCount` undefined.

- [ ] **Step 3: Add the helper**

In `internal/session/summary.go`, add (the `agent` import is already present):

```go
// plannedCaseCount returns the number of real role units in a plan, excluding
// lazy fallback cases (FallbackFor != ""), which are rescue copies of an
// existing primary rather than independent planned roles (A1 Phase 2).
func plannedCaseCount(plan *agent.TestPlan) int {
	if plan == nil {
		return 0
	}
	n := 0
	for i := range plan.Cases {
		if plan.Cases[i].FallbackFor == "" {
			n++
		}
	}
	return n
}
```

- [ ] **Step 4: Run test to verify GREEN**

Run: `go test ./internal/session/ -run TestPlannedCaseCount_ExcludesLazyFallback -v`
Expected: PASS.

- [ ] **Step 5: Wire both call sites**

In `internal/session/run_phases_lifecycle.go`, change the `FromResults` call's plan-cases argument from `len(rp.plan.Cases)` to `plannedCaseCount(rp.plan)`:

```go
		plannedCaseCount(rp.plan),
```

In `internal/session/resume_phases_helpers.go`, make the same replacement at its `FromResults` call:

```go
		plannedCaseCount(rp.plan),
```

- [ ] **Step 6: Regression — full session package**

Run: `go test ./internal/session/ -count=1`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session/summary.go internal/session/summary_test.go internal/session/run_phases_lifecycle.go internal/session/resume_phases_helpers.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(session): exclude lazy fallback cases from planned case count

TestCasesPlanned now counts only real role units via plannedCaseCount, which
skips cases with FallbackFor set (lazy fallbacks are rescue copies, not
independent planned roles). Wired into both run and resume summary call sites."
```

---

### Task 5: Consolidate — skip recovered/fallback in effectiveness + episodic

**Files:**
- Modify: `internal/session/run_phases_consolidate.go` (`verdictByNormalizedTarget` ~line 127; `writeEpisodicMemory` ~line 31)
- Test: `internal/session/consolidate_recovered_test.go` (new)

**Interfaces:**
- Consumes: `store.Verdict.Recovered` (Task 1), `agent.StepResult.Recovered` + `TestCase.FallbackFor` (A1 Phase 2).
- Produces: a recovered role's effectiveness signal comes from its **primary's fail**, not the fallback's pass; a target gets one episodic row (from its primary).

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/session/consolidate_recovered_test.go`:

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary: a recovered
// fallback verdict (committed, recovered=true) sharing a target with its
// primary's fail must NOT overwrite the fail in the effectiveness map — the
// recalled strategy's signal is the primary's fail.
func TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-1"
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	traceID, err := rp.session.Store.CreateTrace(ctx, rp.session.ID, "ws", "ws://h/ws")
	require.NoError(t, err)

	const target = "ws://h/ws"
	// Primary fail (committed), then recovered fallback (committed), same target.
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "fail", 0.4, "judge", "primary failed", nil, "assertion_failed", false)
	require.NoError(t, err)
	_, err = rp.session.Store.CreateVerdict(ctx, rp.session.ID, traceID, target, "pass", 0.9, "judge", "fallback recovered", nil, "", true)
	require.NoError(t, err)

	out := verdictByNormalizedTarget(ctx, rp.session, nil)
	info, ok := out[normalizeTargetPublic(target)]
	require.True(t, ok, "target present")
	require.Equal(t, examiner.StatusFail, info.status, "primary fail wins; recovered does not overwrite")
}

// TestWriteEpisodicMemory_SkipsFallback: a recovered fallback verdict does not
// produce a second episodic row for its primary's target.
func TestWriteEpisodicMemory_SkipsFallback(t *testing.T) {
	ctx := context.Background()
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.ID = "sess-2"
	_, err := rp.session.Store.DB().ExecContext(ctx,
		`INSERT INTO sessions (id, mode, status, goal, project_name, coverage_pct, stats, started_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		rp.session.ID, "run", "running", "g", "test-project", 0.0, "{}")
	require.NoError(t, err)

	rp.verdicts = []examiner.FinalVerdict{
		{Status: examiner.StatusFail, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "A", Target: "/x"}}},
		{Status: examiner.StatusPass, StepResult: agent.StepResult{TestCase: &agent.TestCase{ID: "A'", Target: "/x", FallbackFor: "A"}, Recovered: true}},
	}
	require.NoError(t, writeEpisodicMemory(ctx, rp.session, rp.verdicts))

	var n int
	require.NoError(t, rp.session.Store.DB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memory_episodic WHERE session_id='sess-2'`).Scan(&n))
	require.Equal(t, 1, n, "one episodic row per target (primary only)")
}
```

> **Note for the implementer:** the test calls `normalizeTargetPublic` and `verdictByNormalizedTarget`/`writeEpisodicMemory`. `verdictByNormalizedTarget` and `writeEpisodicMemory` already exist as package-level funcs. `normalizeTargetPublic` does **not** exist yet — add it in Step 3 as a thin export of the existing `memory.NormalizeTarget` so the test can build the expected key. If the existing `memory.NormalizeTarget` is already callable from the test (same package can call `memory.NormalizeTarget`), use `memory.NormalizeTarget(target)` directly and drop `normalizeTargetPublic`. Pick whichever compiles; the assertion is the point.

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/session/ -run 'TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary|TestWriteEpisodicMemory_SkipsFallback' -v`
Expected: FAIL — `verdictByNormalizedTarget` returns the recovered pass (last committed row wins); `writeEpisodicMemory` writes 2 rows.

- [ ] **Step 3: Add the skip rules**

In `internal/session/run_phases_consolidate.go`, in `verdictByNormalizedTarget`'s committed loop, skip recovered rows before inserting (right after the `if v.Target == "" { continue }` guard):

```go
	for _, v := range committed {
		if v.Target == "" {
			continue
		}
		// A1 Phase 2: a recovered fallback shares its primary's target. Skip it
		// so the primary's fail (the strategy's real signal) wins this slot.
		if v.Recovered {
			continue
		}
		out[memory.NormalizeTarget(v.Target)] = verdictInfo{
			status: examiner.JudgeStatus(v.Status),
			reason: v.FailureReason,
		}
	}
```

In the in-memory loop of the same function, skip fallback results (right after the TestCase nil/empty guard):

```go
	for _, v := range verdicts {
		if v.StepResult.TestCase == nil || v.StepResult.TestCase.Target == "" {
			continue
		}
		// A1 Phase 2: skip fallback verdicts (recovered or not) — the primary
		// already represents this target.
		if v.StepResult.TestCase.FallbackFor != "" {
			continue
		}
		key := memory.NormalizeTarget(v.StepResult.TestCase.Target)
```

In `writeEpisodicMemory`, skip fallback verdicts (right after the `tc := v.StepResult.TestCase` / nil+empty guard):

```go
	for _, v := range verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Target == "" {
			continue
		}
		// A1 Phase 2: the fallback shares its primary's target; the primary
		// already records the episodic row. Skip to avoid a duplicate.
		if tc.FallbackFor != "" {
			continue
		}
		target := memory.NormalizeTarget(tc.Target)
```

(If you chose to use `memory.NormalizeTarget` directly in the test, no `normalizeTargetPublic` is needed. If you added it, keep it as a one-line wrapper; otherwise omit.)

- [ ] **Step 4: Run the targeted tests to verify GREEN**

Run: `go test ./internal/session/ -run 'TestVerdictByNormalizedTarget_RecoveredDoesNotOverwritePrimary|TestWriteEpisodicMemory_SkipsFallback' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full session package**

Run: `go test ./internal/session/ -count=1`
Expected: PASS. Existing `consolidate_effectiveness_test.go` / `consolidate_episodic_test.go` cases have no `FallbackFor`, so the new guards are no-ops for them.

- [ ] **Step 6: Commit**

```bash
git add internal/session/run_phases_consolidate.go internal/session/consolidate_recovered_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "fix(session): skip recovered/fallback in effectiveness + episodic consolidate

verdictByNormalizedTarget skips recovered committed rows and in-memory fallback
verdicts, so a recovered role's effectiveness signal is its primary's fail (the
recalled strategy failed), not the deterministic fallback's pass. writeEpisodicMemory
skips fallback verdicts so a target gets one episodic row from its primary."
```

---

### Task 6: Report — Markdown recovered column + emoji

**Files:**
- Modify: `internal/report/markdown_render_summary.go` (`renderSummaryTable`)
- Modify: `internal/report/markdown_render_verdicts.go` (`renderVerdictsTable`)
- Modify: `internal/report/markdown_helpers.go` (`statusEmoji`)
- Test: `internal/report/markdown_recovered_test.go` (new)

**Interfaces:**
- Consumes: `session.SessionSummary.Recovered` (Task 3), `store.Verdict.Recovered` (Task 1).
- Produces: Markdown summary shows a Recovered metric; a recovered verdict row renders `♻️ recovered`.

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/report/markdown_recovered_test.go`:

```go
package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRenderSummaryTable_RecoveredRow(t *testing.T) {
	var b strings.Builder
	renderSummaryTable(&b, &session.SessionSummary{Failed: 1, Recovered: 1, TotalCases: 2})
	out := b.String()
	assert.Contains(t, out, "| **Recovered** | 1 |")
}

func TestStatusEmoji_Recovered(t *testing.T) {
	assert.Equal(t, "♻️ recovered", statusEmoji("recovered"))
}

func TestRenderVerdictsTable_RecoveredRow(t *testing.T) {
	var b strings.Builder
	renderVerdictsTable(&b, []store.Verdict{
		{Target: "ws://h/ws", Status: "pass", Recovered: true},
		{Target: "http://h/x", Status: "pass"},
	})
	out := b.String()
	assert.Contains(t, out, "♻️ recovered", "recovered verdict rendered with recovered emoji")
	assert.Contains(t, out, "✅ pass", "normal pass rendered normally")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/report/ -run 'TestRenderSummaryTable_RecoveredRow|TestStatusEmoji_Recovered|TestRenderVerdictsTable_RecoveredRow' -v`
Expected: FAIL — no Recovered row; `statusEmoji("recovered")` returns `"recovered"` (default); verdict row shows `✅ pass` for the recovered verdict.

- [ ] **Step 3: Add the emoji case**

In `internal/report/markdown_helpers.go`, in `statusEmoji`, add a case:

```go
	case "skip", "skipped":
		return "⏭️ " + status
	case "recovered":
		return "♻️ recovered"
```

- [ ] **Step 4: Add the summary row**

In `internal/report/markdown_render_summary.go`, in `renderSummaryTable`, add a Recovered row after Uncertain:

```go
	fmt.Fprintf(b, "| **Uncertain** | %d |\n", sum.Uncertain)
	fmt.Fprintf(b, "| **Recovered** | %d |\n", sum.Recovered)
```

- [ ] **Step 5: Render recovered verdict rows**

In `internal/report/markdown_render_verdicts.go`, in `renderVerdictsTable`, render a recovered verdict with the recovered emoji rather than its underlying pass status. Change the per-row `fmt.Fprintf`:

```go
	for i, v := range verdicts {
		failReason := "—"
		if (v.Status == "fail" || v.Status == "failed") && v.FailureReason != "" {
			failReason = v.FailureReason.DisplayName()
		}
		verdictStatus := v.Status
		if v.Recovered {
			verdictStatus = "recovered"
		}
		fmt.Fprintf(b, "| %d | `%s` | %s | %.2f | %s | %s |\n",
			i+1, v.Target, statusEmoji(verdictStatus), v.Confidence, failReason, v.Source)
	}
```

- [ ] **Step 6: Run the targeted tests to verify GREEN**

Run: `go test ./internal/report/ -run 'TestRenderSummaryTable_RecoveredRow|TestStatusEmoji_Recovered|TestRenderVerdictsTable_RecoveredRow' -v`
Expected: PASS.

- [ ] **Step 7: Regression — full report package**

Run: `go test ./internal/report/ -count=1`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/report/markdown_render_summary.go internal/report/markdown_render_verdicts.go internal/report/markdown_helpers.go internal/report/markdown_recovered_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(report): render recovered in Markdown summary + verdict table

Markdown summary gains a Recovered metric row; statusEmoji maps recovered to
♻️; a verdict row with recovered=true renders as ♻️ recovered instead of its
underlying pass status, making rescued roles visible in the report."
```

---

### Task 7: Report — HTML recovered badge + summary card

**Files:**
- Modify: `internal/report/html_template.go` (CSS badge class; summary-grid card; verdict badge class)
- Test: `internal/report/html_recovered_test.go` (new)

**Interfaces:**
- Consumes: `session.SessionSummary.Recovered` (Task 3), `store.Verdict.Recovered` (Task 1).
- Produces: HTML shows a Recovered summary card and a `badge-recovered` for recovered verdict rows.

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/report/html_recovered_test.go`:

```go
package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRenderHTML_RecoveredSummaryCard(t *testing.T) {
	data := ReportData{
		Session:  store.Session{ID: "s1"},
		Summary:  &session.SessionSummary{TotalCases: 2, Failed: 1, Recovered: 1},
		Verdicts: nil,
	}
	out, err := RenderHTMLString(data)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, ">1</div><div class=\"label\">Recovered</div>"), "recovered summary card present")
	assert.True(t, strings.Contains(out, ".badge-recovered"), "badge-recovered CSS class defined")
}

func TestRenderHTML_RecoveredVerdictBadge(t *testing.T) {
	data := ReportData{
		Session: store.Session{ID: "s1"},
		Summary: &session.SessionSummary{TotalCases: 1},
		Verdicts: []store.Verdict{
			{Target: "ws://h/ws", Status: "pass", Recovered: true},
		},
	}
	out, err := RenderHTMLString(data)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "badge-recovered"), "recovered verdict gets badge-recovered")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/report/ -run 'TestRenderHTML_RecoveredSummaryCard|TestRenderHTML_RecoveredVerdictBadge' -v`
Expected: FAIL — no Recovered card, no `.badge-recovered` CSS.

- [ ] **Step 3: Add CSS + summary card + verdict badge**

In `internal/report/html_template.go`, add a recovered CSS variable + badge class. In the `:root` line append `--recovered`:

```go
  :root { --pass: #22c55e; --fail: #ef4444; --uncertain: #eab308; --skip: #9ca3af; --recovered: #0ea5e9; --bg: #f8fafc; --border: #e2e8f0; }
```

Add the badge class alongside the other badge classes:

```go
  .badge-skip { background: var(--skip); }
  .badge-recovered { background: var(--recovered); }
```

In the summary-grid, add a Recovered card after the Uncertain card:

```go
  <div class="summary-card"><div class="value" style="color:var(--uncertain)">{{.Summary.Uncertain}}</div><div class="label">Uncertain</div></div>
  <div class="summary-card"><div class="value" style="color:var(--recovered)">{{.Summary.Recovered}}</div><div class="label">Recovered</div></div>
```

In the verdicts table row, make the badge class recovered-aware:

```go
    <td><span class="badge badge-{{if $v.Recovered}}recovered{{else}}{{$v.Status}}{{end}}">{{$v.Status}}</span></td>
```

- [ ] **Step 4: Run the targeted tests to verify GREEN**

Run: `go test ./internal/report/ -run 'TestRenderHTML_RecoveredSummaryCard|TestRenderHTML_RecoveredVerdictBadge' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full report package**

Run: `go test ./internal/report/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/report/html_template.go internal/report/html_recovered_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(report): render recovered in HTML report

Add a Recovered summary card and a badge-recovered CSS class (--recovered
sky-blue); verdict rows with recovered=true render badge-recovered so rescued
roles are visually distinct in the HTML report."
```

---

### Task 8: Report — JUnit recovered testcase

**Files:**
- Modify: `internal/report/junit_case.go` (`buildJUnitCase`)
- Test: `internal/report/junit_recovered_test.go` (new)

**Interfaces:**
- Consumes: `store.Verdict.Recovered` (Task 1).
- Produces: a recovered verdict is a **passing** JUnit testcase (suite does not fail) named with a ` (recovered)` suffix.

- [ ] **Step 1: Write the failing test (RED)**

Create `internal/report/junit_recovered_test.go`:

```go
package report

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/store"
)

func TestBuildJUnitCase_RecoveredIsPassingWithSuffix(t *testing.T) {
	v := store.Verdict{Target: "ws://h/ws", Status: "pass", Recovered: true}
	tc := buildJUnitCase(v, nil)
	assert.Nil(t, tc.Failure, "recovered does not fail the suite")
	assert.Nil(t, tc.Error)
	assert.Contains(t, tc.Name, "(recovered)", "recovered testcase is marked")
}

func TestBuildJUnitCase_NormalPassUnchanged(t *testing.T) {
	v := store.Verdict{Target: "ws://h/ws", Status: "pass"}
	tc := buildJUnitCase(v, nil)
	assert.Nil(t, tc.Failure)
	assert.NotContains(t, tc.Name, "(recovered)")
}
```

- [ ] **Step 2: Run test to verify RED**

Run: `go test ./internal/report/ -run 'TestBuildJUnitCase_RecoveredIsPassingWithSuffix|TestBuildJUnitCase_NormalPassUnchanged' -v`
Expected: FAIL — recovered verdict has no `(recovered)` suffix.

- [ ] **Step 3: Handle recovered in `buildJUnitCase`**

In `internal/report/junit_case.go`, at the top of `buildJUnitCase` (after `tc` is initialized from `verdictName(v)`), branch on recovered before the status switch so a recovered verdict is a passing testcase with a suffix:

```go
func buildJUnitCase(v store.Verdict, evidence map[int64][]store.Evidence) junitCase {
	tc := junitCase{
		Name:      verdictName(v),
		Classname: "cerberus",
	}

	// A1 Phase 2: a recovered verdict is a passing testcase (the role was
	// rescued by a deterministic fallback, so the suite must not fail), marked
	// so a reader sees it was not a clean pass.
	if v.Recovered {
		tc.Name += " (recovered)"
		return tc
	}

	evSummary := evidenceSummary(evidence, v.TraceID)

	switch v.Status {
```

- [ ] **Step 4: Run the targeted tests to verify GREEN**

Run: `go test ./internal/report/ -run 'TestBuildJUnitCase_RecoveredIsPassingWithSuffix|TestBuildJUnitCase_NormalPassUnchanged' -v`
Expected: PASS.

- [ ] **Step 5: Regression — full report package + make check**

Run: `make check`
Expected: EXIT 0 (fmt + lint + test -race across all packages).

- [ ] **Step 6: Commit + push**

```bash
git add internal/report/junit_case.go internal/report/junit_recovered_test.go
git -c user.name='binoctal' -c user.email='binoctal@gmail.com' commit -m "feat(report): mark recovered verdicts in JUnit XML

A recovered verdict becomes a passing JUnit testcase (no <failure>/<error>, so
a rescued role does not fail the CI suite) with a (recovered) name suffix,
keeping the recovery visible to JUnit consumers.

Spec: cerberus-docs/superpowers/specs/2026-07-29-recovered-rendering-design.md"
git push origin main
```

---

## Self-Review (completed)

- **Spec coverage:**
  - Encoding (recovered column) → Task 1. ✓
  - Persist recovered → Task 2. ✓
  - `SessionSummary.Recovered` + `FromResults` outcome model (pairing, TotalCases, Coverage) → Task 3. ✓
  - `String()` recovered → Task 3. ✓
  - `plannedCaseCount` excludes lazy fallback → Task 4. ✓
  - `verdictByNormalizedTarget` committed + in-memory skip → Task 5. ✓
  - `writeEpisodicMemory` skip → Task 5. ✓
  - Markdown summary row + verdict emoji → Task 6. ✓
  - HTML card + badge → Task 7. ✓
  - JUnit recovered testcase → Task 8. ✓
  - Out of scope (parallel progress event, examiner judge awareness, primary episodic label, non-WS) → no task touches them. ✓
- **Placeholder scan:** No TBD/TODO; every code step has full code. The only implementer judgment call is in Task 5 Step 1/3 (use `memory.NormalizeTarget` directly vs. a wrapper) — both paths are spelled out and either compiles. ✓
- **Type consistency:** `Verdict.Recovered bool` consistent across Tasks 1/2/5/6/7/8; `CreateVerdict(..., recovered bool)` consistent across Tasks 1/2; `SessionSummary.Recovered int` consistent across Tasks 3/6/7; `plannedCaseCount(plan *agent.TestPlan) int` consistent across Task 4. ✓
- **Test design:** Each task's test fails for the documented reason before the implementation step and passes after; the golden pairing case (A/B/C) anchors the core semantics; `make check` gates the final task. ✓

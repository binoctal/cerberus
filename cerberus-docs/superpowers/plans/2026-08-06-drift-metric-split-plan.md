# Drift Metric Split Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the Examiner validation harness's single boolean `drift` into four categories (incorrect / honest-uncertain / under-confident / clean) so honest-uncertain verdicts are no longer penalized, and re-read both validation docs under the new metric.

**Architecture:** A pure helper `classifyDrift(status, conf, threshold)` (in a `_test.go` file, so it never enters the production binary) replaces the inline boolean. The manual validation test accumulates four counters per run and reports two drift totals — `old_drift` (backward-compatible) and `new_drift` (excludes honest-uncertain). No examiner product code changes; the two validation docs are rewritten from the already-on-disk raw data under the new metric.

**Tech Stack:** Go 1.25, table-driven tests, existing manual validation harness (`//go:build manual`).

## Global Constraints

- Module: `github.com/binoctal/cerberus`, Go 1.25, no CGo.
- Commit author: `binoctal <binoctal@gmail.com>`, NO `Co-Authored-By`.
- Code comments and commit messages in English; match existing comment density.
- `classifyDrift` lives in a `_test.go` file (validation-only, not in the production binary).
- All docs go in `cerberus-docs/`, never `docs/`.

---

## File Structure

- `internal/head/examiner/drift_classify_test.go` — NEW. Defines `classifyDrift` AND its table test. `_test.go` keeps it out of the binary; no `//go:build manual` tag so `make test` runs the unit test by default.
- `internal/head/examiner/vocab_validation_manual_test.go` — MODIFIED. Replaces the inline drift boolean with `classifyDrift`; four counters; new output format.
- `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md` — MODIFIED. Four-category drift definition; correction section reversing the prior "dimension did not reduce drift" conclusion.
- `cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md` — MODIFIED. Four-category drift definition; re-evaluated conclusion from re-bucketed data.

---

### Task 1: `classifyDrift` helper + table test (TDD)

**Files:**
- Create: `internal/head/examiner/drift_classify_test.go`

**Interfaces:**
- Produces: `func classifyDrift(status JudgeStatus, conf, threshold float64) string` — returns one of `"incorrect" | "honest-uncertain" | "under-confident" | "clean"`. Task 2 consumes it.

- [ ] **Step 1: Write the failing table test**

Create `internal/head/examiner/drift_classify_test.go`:

```go
package examiner

import "testing"

// TestClassifyDrift covers all four categories plus the boundary case where
// conf == threshold (clean, since the under-confident check is strict <).
func TestClassifyDrift(t *testing.T) {
	const th = 0.9
	tests := []struct {
		name   string
		status JudgeStatus
		conf   float64
		want   string
	}{
		{"incorrect is fail regardless of conf", StatusFail, 0.99, "incorrect"},
		{"honest-uncertain", StatusUncertain, 0.30, "honest-uncertain"},
		{"under-confident is pass below threshold", StatusPass, 0.80, "under-confident"},
		{"clean is pass at or above threshold", StatusPass, 0.95, "clean"},
		{"boundary conf==threshold is clean", StatusPass, th, "clean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDrift(tc.status, tc.conf, th); got != tc.want {
				t.Errorf("classifyDrift(%s, %.2f, %.1f) = %q, want %q",
					tc.status, tc.conf, th, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/head/examiner/ -run TestClassifyDrift -v`
Expected: FAIL — `undefined: classifyDrift`.

- [ ] **Step 3: Implement `classifyDrift`**

Append to `internal/head/examiner/drift_classify_test.go` (above the test, same file):

```go
// classifyDrift sorts one judge verdict into one of four drift categories.
// Ground truth is pass for every validation case, so fail is the only incorrect
// verdict; uncertain is treated as honest (exclusion-gated claims are genuinely
// unproven until an active probe lands), and a low-confidence pass is
// under-confident. The asymmetry is intentional: an under-confident pass is an
// unreliable correct (the next run may flip it), while an honest-uncertain
// verdict is a reliable unknowable.
func classifyDrift(status JudgeStatus, conf, threshold float64) string {
	switch {
	case status == StatusFail:
		return "incorrect"
	case status == StatusUncertain:
		return "honest-uncertain"
	case status == StatusPass && conf < threshold:
		return "under-confident"
	default:
		return "clean"
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/head/examiner/ -run TestClassifyDrift -v`
Expected: PASS — all subtests.

- [ ] **Step 5: Run the full examiner package (no-regression)**

Run: `go test ./internal/head/examiner/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/head/examiner/drift_classify_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "feat(examiner): classifyDrift helper for four-category drift split"
```

---

### Task 2: Wire `classifyDrift` into the manual validation test

**Files:**
- Modify: `internal/head/examiner/vocab_validation_manual_test.go` (the `for _, c := range cases` loop and the two summaries)

**Interfaces:**
- Consumes: `classifyDrift` (Task 1).
- Produces: per-run/per-condition output now carries the four counts and both drift totals.

- [ ] **Step 1: Replace the per-case drift boolean with the four counters**

In `vocab_validation_manual_test.go`, find the loop body (currently):

```go
				driftCount := 0
				var lines []string
				for _, c := range cases {
					vr, err := judge.Judge(context.Background(), c.result)
					require.NoError(t, err, "judge failed for case %q", c.name)
					drift := vr.Status != StatusPass || vr.CorrectnessConfidence < driftThreshold
					if drift {
						driftCount++
					}
					lines = append(lines, fmt.Sprintf("  %-14s status=%-9s conf=%.2f drift=%v", c.name, vr.Status, vr.CorrectnessConfidence, drift))
				}
				summary := fmt.Sprintf("[%s] cases=%d drift=%d", label, len(cases), driftCount)
```

Replace it with:

```go
				var incorrect, honest, underconf int
				var lines []string
				for _, c := range cases {
					vr, err := judge.Judge(context.Background(), c.result)
					require.NoError(t, err, "judge failed for case %q", c.name)
					cat := classifyDrift(vr.Status, vr.CorrectnessConfidence, driftThreshold)
					switch cat {
					case "incorrect":
						incorrect++
					case "honest-uncertain":
						honest++
					case "under-confident":
						underconf++
					}
					lines = append(lines, fmt.Sprintf("  %-14s status=%-9s conf=%.2f cat=%s", c.name, vr.Status, vr.CorrectnessConfidence, cat))
				}
				oldDrift := incorrect + honest + underconf
				newDrift := incorrect + underconf
				summary := fmt.Sprintf("[%s] cases=%d incorrect=%d honest=%d underconf=%d new_drift=%d old_drift=%d",
					label, len(cases), incorrect, honest, underconf, newDrift, oldDrift)
```

- [ ] **Step 2: Verify the manual build compiles**

Run: `go vet -tags=manual ./internal/head/examiner/`
Expected: clean (no diagnostics).

- [ ] **Step 3: Verify the default build still passes**

Run: `go test ./internal/head/examiner/`
Expected: PASS — `classifyDrift` is used only under `-tags=manual`, but the default build must still compile and pass.

- [ ] **Step 4: Commit**

```bash
git add internal/head/examiner/vocab_validation_manual_test.go
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "test(examiner): report four-category drift in manual validation"
```

---

### Task 3: Re-read the structured-evidence validation doc under the new metric

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md`

**Interfaces:**
- Consumes: the raw run files already on disk at `runtime/examiner-vocab-validation/` (the `vocab-dim`/`vocab-strip`/`novocab-dim`/`novocab-strip` runs from the prior validation).

- [ ] **Step 1: Re-bucket the raw data to confirm the numbers**

Run this script and keep its output — it is the source of truth for the rewritten tables:

```bash
python3 - <<'EOF'
import os, re
from collections import defaultdict
base = "runtime/examiner-vocab-validation"
runs = defaultdict(list)
line_re = re.compile(r"^\s+(\S+)\s+status=(\S+)\s+conf=([\d.]+)")
for fn in sorted(os.listdir(base)):
    if not fn.endswith(".txt") or "-run" not in fn:
        continue
    cond = fn.replace(".txt","").rsplit("-run",1)[0]
    inc=huc=uc=0; n=0
    for line in open(os.path.join(base,fn)):
        m=line_re.match(line)
        if not m: continue
        n+=1; status,conf=m.group(2),float(m.group(3))
        if status=="fail": inc+=1
        elif status=="uncertain": huc+=1
        elif status=="pass" and conf<0.9: uc+=1
    runs[cond].append((inc,huc,uc,n))
print(f"{'condition':14} {'N':4} {'incorrect':9} {'honest-unc':10} {'under-conf':10} {'old':4} {'new':4}")
for cond in sorted(runs):
    rs=runs[cond]; N=sum(r[3] for r in rs)
    inc=sum(r[0] for r in rs); huc=sum(r[1] for r in rs); uc=sum(r[2] for r in rs)
    print(f"{cond:14} {N:<4} {inc:<9} {huc:<10} {uc:<10} {inc+huc+uc:<4} {inc+uc}")
EOF
```

Expected output (the values to put in the doc):

```
condition      N    incorrect honest-unc under-conf old new
novocab-dim    15   0         5          1          6    1
novocab-strip  15   0         2          4          6    4
vocab-dim      15   0         3          1          4    1
vocab-strip    15   0         0          4          4    4
```

- [ ] **Step 2: Update the drift definition line**

In the doc, replace:

```
`drift = Status != pass OR CorrectnessConfidence < 0.9`.
```

with:

```
`drift` is split into four categories (ground truth is `pass` for every case):
`incorrect` (fail), `honest-uncertain` (uncertain), `under-confident` (pass but
`conf < 0.9`), `clean` (pass at `conf >= 0.9`). The primary metric is
`new_drift = incorrect + under-confident`; `old_drift = incorrect +
honest-uncertain + under-confident` is kept for backward comparison.
```

- [ ] **Step 3: Add the category breakdown table after the existing "Overall drift" table**

Insert after the `## Overall drift` table:

```
## Category breakdown (re-bucketed)

| Condition     | incorrect | honest-uncertain | under-confident | old_drift | new_drift |
|---------------|-----------|------------------|-----------------|-----------|-----------|
| vocab-dim     | 0         | 3                | 1               | 4         | 1         |
| vocab-strip   | 0         | 0                | 4               | 4         | 4         |
| novocab-dim   | 0         | 5                | 1               | 6         | 1         |
| novocab-strip | 0         | 2                | 4               | 6         | 4         |
```

- [ ] **Step 4: Add a correction section reversing the conclusion**

Append a new section after `## Conclusion (honest)`:

```
## Correction (2026-08-06, metric split)

The drift metric used above conflated `honest-uncertain` with real errors. Under
the four-category split, `incorrect` is 0 in every condition — the judge never
mislabels a pass-case as `fail`. The dimension's effect was to convert
under-confident passes into honest-uncertain verdicts (it surfaces `sender
exclusion not probed`, so a fan-out expectation yields `uncertain`). That is
correct epistemic behavior the old metric penalized.

Under `new_drift` (excluding honest-uncertain): vocab-dim 1 vs vocab-strip 4;
novocab-dim 1 vs novocab-strip 4. **The dimension does reduce drift — the prior
"did not reduce drift" conclusion is reversed.** The binding constraint for the
remaining fanout/routing drift is still the deferred sender-exclusion probe; the
metric fix changes how we read the dimension, not what it can prove.
```

- [ ] **Step 5: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-06-structured-evidence-validation.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "docs(validation): re-read structured-evidence drift under four-category split"
```

---

### Task 4: Re-read the vocab validation doc under the new metric

**Files:**
- Modify: `cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md`

**Interfaces:**
- Consumes: the raw run files at `runtime/examiner-vocab-validation/` named `with-vocab-run*` / `without-vocab-run*` (the older 4-case validation).

- [ ] **Step 1: Re-bucket the vocab-condition data**

Run:

```bash
python3 - <<'EOF'
import os, re
from collections import defaultdict
base = "runtime/examiner-vocab-validation"
runs = defaultdict(list)
line_re = re.compile(r"^\s+(\S+)\s+status=(\S+)\s+conf=([\d.]+)")
for fn in sorted(os.listdir(base)):
    if not fn.endswith(".txt") or "-run" not in fn:
        continue
    cond = fn.replace(".txt","").rsplit("-run",1)[0]
    if cond not in ("with-vocab","without-vocab"):
        continue
    inc=huc=uc=0; n=0
    for line in open(os.path.join(base,fn)):
        m=line_re.match(line)
        if not m: continue
        n+=1; status,conf=m.group(2),float(m.group(3))
        if status=="fail": inc+=1
        elif status=="uncertain": huc+=1
        elif status=="pass" and conf<0.9: uc+=1
    runs[cond].append((inc,huc,uc,n))
for cond in sorted(runs):
    rs=runs[cond]; N=sum(r[3] for r in rs)
    inc=sum(r[0] for r in rs); huc=sum(r[1] for r in rs); uc=sum(r[2] for r in rs)
    print(f"{cond:14} N={N} incorrect={inc} honest-unc={huc} under-conf={uc} old={inc+huc+uc} new={inc+uc}")
EOF
```

Expected (the `routing` case is the recurring drift; record whether it lands as `honest-uncertain` or `under-confident` per condition — write the conclusion from whatever these numbers show).

- [ ] **Step 2: Update the drift definition on line 8**

Replace:

```
- `N=3` runs per condition, drift threshold 0.9 (`drift = Status != pass OR CorrectnessConfidence < 0.9`).
```

with:

```
- `N=3` runs per condition, drift threshold 0.9. `drift` is reported as four
  categories: `incorrect` (fail), `honest-uncertain` (uncertain), `under-confident`
  (pass but `conf < 0.9`), `clean`. Primary metric `new_drift = incorrect +
  under-confident`; `old_drift` (all non-clean) kept for comparison.
```

- [ ] **Step 3: Re-evaluate the conclusion from the re-bucketed numbers**

The original conclusion (line 33: "Both conditions drift `1/4` on every run...
Vocabulary injection did not reduce judge drift") was read under the old metric.
Rewrite the "Conclusion" section to state, from the Step 1 output:
- Whether `routing` (the recurring drift) is `honest-uncertain` or `under-confident` in each condition.
- The `new_drift` for `with-vocab` vs `without-vocab`.
- An honest verdict on whether vocab reduces `new_drift` on this case set.

Do not predetermine the direction — write what the numbers say. If the numbers
show vocab still does not reduce `new_drift`, say so; if they reverse, say that.

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/technical/validation/2026-08-06-examiner-vocab-validation.md
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "docs(validation): re-read examiner vocab drift under four-category split"
```

---

### Task 5: Full verification

- [ ] **Step 1: Format and vet**

Run: `make fmt && go vet ./... && go vet -tags=manual ./...`
Expected: clean.

- [ ] **Step 2: Lint**

Run: `make lint`
Expected: clean.

- [ ] **Step 3: Full test suite**

Run: `make test`
Expected: PASS, race-clean — including the new `TestClassifyDrift`.

- [ ] **Step 4: Commit any fmt drift**

```bash
git add -A
git -c user.name=binoctal -c user.email=binoctal@gmail.com \
  commit -m "chore: fmt after drift metric split" || echo "nothing to commit"
```

---

## Self-Review Notes

- **Spec coverage:** `classifyDrift` four-category set → Task 1. Validation harness wiring + output format → Task 2. structured-evidence doc rewrite (incl. reversal) → Task 3. vocab doc re-evaluation → Task 4. Verification → Task 5. The honest-uncertain asymmetry rationale is embedded in the `classifyDrift` doc comment (Task 1 Step 3) and the structured-evidence correction (Task 3 Step 4).
- **Placeholder scan:** every code/doc step shows actual content; the only "write what the numbers say" instruction (Task 4 Step 3) is intentional — that doc's conclusion is data-driven and must not be predetermined.
- **Type consistency:** `classifyDrift(JudgeStatus, float64, float64) string` is defined in Task 1 and consumed identically in Task 2. Return values `"incorrect" | "honest-uncertain" | "under-confident" | "clean"` match the switch cases in Task 2 and the python re-bucket in Task 3.
- **Out of scope (per spec):** sender-exclusion probe, per-case evidence-sufficiency annotation, threshold/run-count changes — none added.

# AutoTest Report Integration Design

**Date**: 2026-06-15
**Status**: Design
**Related**: [2026-06-14-autotest-coverage-design.md](./2026-06-14-autotest-coverage-design.md)

## Overview

Integrate AutoTest execution results into `cerberus report` output, enabling users to review test generation outcomes, coverage improvements, and file changes through DB-persisted reports in markdown/HTML/JSON formats.

**Design Philosophy**: GitHub Actions Job Summary style - concise summary tables at top, detailed item lists below, native markdown format for direct PR embed.

## Background

AutoTest phase (completed in commit 704ab0c) currently only stores results in memory (`session.LastAutoTestReport`). The `cerberus report` command reads from DB, so it cannot access AutoTest data. This design adds persistence and reporting layers.

### Current State

- `internal/autotest/` package: Runs `go test -coverprofile`, finds gaps (0% functions + no test files), generates `_test.go` files via AI, validates coverage improvements, reverts if no improvement
- Report exists only in memory: `session.LastAutoTestReport`
- `cerberus report` reads from DB via `store.GetSession`
- Dry-run mode prints generated code to stdout (commit 19d6c80)

### Goals

1. Persist AutoTest results to database
2. Integrate into `cerberus report` (markdown/HTML/JSON)
3. Provide actionable insights: what was tested, what improved, what was reverted

## Design Decisions

### Report Style: GitHub Actions Job Summary

After evaluating CI/CD platforms (GitHub Actions, GitLab, CircleCI), we adopt **GitHub Actions Job Summary** style:

- **Rationale**:
  - Native markdown format, no XML conversion layer needed
  - Familiar to developers (PR comments, CI summaries)
  - Fits Cerberus's `--format=markdown` output
  - Aligns with user preference: "只列清单表，不预览代码"

- **Alternatives Considered**:
  - **GitLab JUnit XML**: Standard format but requires XML parser/renderer. Overkill for current needs.
  - **CircleCI Insights**: Heavy on historical trends. AutoTest focuses on single-run improvements.

### Content: Summary Table + Item List (No Code Preview)

Per user feedback, report shows **summary + item list only**, not full code preview:

```
| Before → After Coverage | 62.0% → 71.5% |
| Written / Reverted / Skipped / Failed | 3 / 1 / 0 / 1 |

| # | Test File | Target | Reason | Status |
|---|-----------|--------|--------|--------|
| 1 | bar_test.go | Bar (bar.go) | 0% covered | ✅ written |
| 2 | baz_test.go | Baz (baz.go) | no test file | ❌ reverted |
```

- **Why not code preview?**:
  - Dry-run: already printed to stdout
  - Auto/approve: files exist on disk, use `git diff` or read file
  - DB storing code bytes is redundant (MaxGaps=5, but still unnecessary)

### Persistence: Single JSON Column

**Approach A (Recommended)**: Single `TEXT` column storing full `AutoTestReport` JSON (without code bytes).

- **Why not separate tables?** Over-normalization for MaxGaps=5 use case. YAGNI.
- **Why not disk-only?** Dry-run never writes files; reverted files are `os.Remove`'d. DB is source of truth for `cerberus report`.

## Data Model

### New Type: `AutoTestItem`

**File**: `internal/autotest/types.go`

```go
// AutoTestItem represents a single gap's target and result
type AutoTestItem struct {
    TargetFile string // gap.File - source file being tested
    TargetFunc string // gap.Func - function being tested
    Reason     string // "0% covered" | "no test file" - why generated
    TestPath   string // generated _test.go path (empty if failed)
    Status     string // "written" | "reverted" | "skipped" | "failed" | "generated"
}
```

**Status Values**:
- `written`: Generated, written to disk, coverage improved (kept)
- `reverted`: Generated, written, but coverage didn't improve (removed)
- `skipped`: EscalationGate denied (approve mode)
- `failed`: Generation or write error
- `generated`: Dry-run mode (generated but not written)

### Updated `AutoTestReport`

```go
type AutoTestReport struct {
    // Existing fields (for dry-run stdout)
    Gaps      []Gap
    Generated []TestFile
    Written   []string
    Reverted  []string
    Skipped   []string
    Failed    []string

    // New: Per-item aligned records
    Items []AutoTestItem

    // Existing
    BeforeCoveragePct float64
    AfterCoveragePct float64
    Timestamp time.Time
}

type TestFile struct {
    Path    string
    Content []byte `json:"-"` // Don't marshal to DB (dry-run stdout reads from memory)
}
```

**Rationale**:
- Keep existing fields for backward compatibility (dry-run stdout, logs)
- `Content []byte json:"-"` prevents marshaling code bytes to DB
- `Items []AutoTestItem` aligns target + result per gap

## Implementation

### 1. Migration V005

**File**: `migrations/V005__autotest_report.sql`

```sql
ALTER TABLE sessions ADD COLUMN autotest_report TEXT NOT NULL DEFAULT '';
```

**Pattern**: Follows V004 (`ALTER TABLE sessions ADD COLUMN stats TEXT`).

### 2. Store Layer

**File**: `internal/store/session.go`

```go
type Session struct {
    // existing...
    AutoTestReport string `json:"autotest_report,omitempty"`
}

// GetSession selects autotest_report column
func GetSession(ctx context.Context, db *sql.DB, id string) (*Session, error) {
    // SELECT id, created_at, ..., autotest_report FROM sessions WHERE id = ?
}

// ListSessions includes autotest_report in SELECT
func ListSessions(ctx context.Context, db *sql.DB, filter SessionFilter) ([]Session, error) {
    // SELECT id, created_at, ..., autotest_report FROM sessions ...
}

// UpdateSessionAutoTest persists AutoTest report JSON
func UpdateSessionAutoTest(ctx context.Context, db *sql.DB, id string, report any) error {
    data, err := jsonText(report)
    if err != nil {
        return err
    }
    _, err = db.ExecContext(ctx, `UPDATE sessions SET autotest_report = ? WHERE id = ?`, data, id)
    return err
}
```

**Design Notes**:
- Store doesn't import `autotest` package, uses `any` + `jsonText` marshaler
- `NOT NULL DEFAULT ''` ensures backward compatibility (no COALESCE needed)

### 3. Run Flow Updates

**File**: `internal/autotest/autotest.go`

```go
func (r *Run) Execute() (*AutoTestReport, error) {
    rep := &AutoTestReport{}

    for _, gap := range r.Gaps {
        item := AutoTestItem{
            TargetFile: gap.File,
            TargetFunc: gap.Func,
            Reason:     gap.Reason, // "0% covered" | "no test file"
            Status:     "failed",   // default
        }

        // Generate
        tf, err := r.generateTest(gap)
        if err != nil {
            item.Status = "failed"
            rep.Items = append(rep.Items, item)
            rep.Failed = append(rep.Failed, gap.String())
            continue
        }

        item.TestPath = tf.Path
        item.Status = "generated" // default for dry-run

        // Gate check (approve mode)
        if r.Mode == "approve" && !r.Gate.Allow(tf) {
            item.Status = "skipped"
            rep.Items = append(rep.Items, item)
            rep.Skipped = append(rep.Skipped, tf.Path)
            continue
        }

        // Write
        if r.Mode != "dry-run" {
            if err := r.writeTest(tf); err != nil {
                item.Status = "failed"
                rep.Items = append(rep.Items, item)
                rep.Failed = append(rep.Failed, tf.Path)
                continue
            }

            // Verify coverage improvement
            if improved := r.verifyCoverage(); improved {
                item.Status = "written"
                rep.Written = append(rep.Written, tf.Path)
            } else {
                item.Status = "reverted"
                rep.Reverted = append(rep.Reverted, tf.Path)
                os.Remove(tf.Path)
            }
        }

        rep.Items = append(rep.Items, item)
        rep.Generated = append(rep.Generated, tf)
    }

    rep.BeforeCoveragePct = r.initialCoverage
    rep.AfterCoveragePct = r.measureCoverage()
    return rep, nil
}
```

**Key Changes**:
- Maintain `item` per gap, update `TestPath` and `Status` as phases progress
- Append to `rep.Items` at end of each gap iteration
- Keep existing slices (`Written`, `Reverted`, etc.) for dry-run stdout

### 4. Lifecycle Persistence

**File**: `internal/lifecycle/lifecycle.go` (Phase 4)

```go
// Phase 4: AutoTest
if s.Config.AutoTestSafety != "off" {
    atReport, atErr := s.AutoTest.Run(ctx)
    s.LastAutoTestReport = atReport

    // Persist to DB
    if atReport != nil {
        if perr := s.Store.UpdateSessionAutoTest(ctx, s.ID, atReport); perr != nil {
            s.Logger.Warn("persist autotest report", zap.Error(perr))
        }
    }

    if atErr != nil {
        return s.phase.Error(atErr)
    }
}
```

**Error Handling**:
- Best-effort persistence: Warn on DB error, don't fail the run
- Persist even if phase partially fails (report has partial data)

### 5. Report Building

**File**: `internal/report/report.go`

```go
type ReportData struct {
    // existing...
    AutoTest *autotest.AutoTestReport `json:"autotest,omitempty"`
}

func BuildReport(ctx context.Context, sess *store.Session, files []store.File) (*ReportData, error) {
    data := &ReportData{}

    // existing...

    if sess.AutoTestReport != "" {
        var atReport autotest.AutoTestReport
        if err := json.Unmarshal([]byte(sess.AutoTestReport), &atReport); err != nil {
            // Log but don't fail - report other sections still useful
            ctx := ctx.(*lifecycle.Context)
            ctx.Logger.Warn("unmarshal autotest report", zap.Error(err))
        } else {
            data.AutoTest = &atReport
        }
    }

    return data, nil
}
```

### 6. Markdown Section

**File**: `internal/report/markdown.go`

```go
func (m *Markdown) renderAutoTest(data *report.ReportData) string {
    if data.AutoTest == nil || len(data.AutoTest.Items) == 0 {
        return ""
    }

    var buf bytes.Buffer

    // Summary header
    buf.WriteString("## AutoTest\n\n")

    // Coverage summary
    buf.WriteString(fmt.Sprintf("| Before → After Coverage | %.1f%% → %.1f%% |\n",
        data.AutoTest.BeforeCoveragePct, data.AutoTest.AfterCoveragePct))

    // Status summary
    written := countStatus(data.AutoTest.Items, "written")
    reverted := countStatus(data.AutoTest.Items, "reverted")
    skipped := countStatus(data.AutoTest.Items, "skipped")
    failed := countStatus(data.AutoTest.Items, "failed")
    buf.WriteString(fmt.Sprintf("| Written / Reverted / Skipped / Failed | %d / %d / %d / %d |\n\n",
        written, reverted, skipped, failed))

    // Item table
    buf.WriteString("| # | Test File | Target | Reason | Status |\n")
    buf.WriteString("|---|-----------|--------|--------|--------|\n")

    for i, item := range data.AutoTest.Items {
        statusBadge := m.statusBadge(item.Status)
        buf.WriteString(fmt.Sprintf("| %d | `%s` | %s (`%s`) | %s | %s |\n",
            i+1,
            item.TestPath,
            item.TargetFunc,
            filepath.Base(item.TargetFile),
            item.Reason,
            statusBadge))
    }

    buf.WriteString("\n")
    return buf.String()
}

func (m *Markdown) statusBadge(status string) string {
    switch status {
    case "written":
        return "✅ written"
    case "reverted":
        return "❌ reverted"
    case "skipped":
        return "⏭️ skipped"
    case "failed":
        return "💥 failed"
    case "generated":
        return "📝 generated"
    default:
        return status
    }
}

func countStatus(items []autotest.AutoTestItem, status string) int {
    count := 0
    for _, item := range items {
        if item.Status == status {
            count++
        }
    }
    return count
}
```

**Sample Output**:

```markdown
## AutoTest

| Before → After Coverage | 62.0% → 71.5% |
| Written / Reverted / Skipped / Failed | 3 / 1 / 0 / 1 |

| # | Test File | Target | Reason | Status |
|---|-----------|--------|--------|--------|
| 1 | `internal/foo/bar_test.go` | `Bar` (`bar.go`) | 0% covered | ✅ written |
| 2 | `internal/baz/baz_test.go` | `Baz` (`baz.go`) | no test file | ❌ reverted |
| 3 | `internal/qux/qux_test.go` | `Qux` (`qux.go`) | 0% covered | 💥 failed |
```

### 7. HTML Section

**File**: `internal/report/html.go`

```go
func (h *HTML) renderAutoTest(data *report.ReportData) string {
    if data.AutoTest == nil || len(data.AutoTest.Items) == 0 {
        return ""
    }

    var buf bytes.Buffer

    // Section header
    buf.WriteString(`<h2>AutoTest</h2>`)

    // Summary cards
    buf.WriteString(`<div class="summary-cards">`)
    buf.WriteString(fmt.Sprintf(`<div class="card"><span class="label">Coverage</span><span class="value">%.1f%% → %.1f%%</span></div>`,
        data.AutoTest.BeforeCoveragePct, data.AutoTest.AfterCoveragePct))
    buf.WriteString(fmt.Sprintf(`<div class="card"><span class="label">Status</span><span class="value">%d written, %d reverted, %d skipped, %d failed</span></div>`,
        countStatus(data.AutoTest.Items, "written"),
        countStatus(data.AutoTest.Items, "reverted"),
        countStatus(data.AutoTest.Items, "skipped"),
        countStatus(data.AutoTest.Items, "failed")))
    buf.WriteString(`</div>`)

    // Item table
    buf.WriteString(`<table class="autotest-table">`)
    buf.WriteString(`<thead><tr><th>#</th><th>Test File</th><th>Target</th><th>Reason</th><th>Status</th></tr></thead>`)
    buf.WriteString(`<tbody>`)

    for i, item := range data.AutoTest.Items {
        buf.WriteString(`<tr>`)
        buf.WriteString(fmt.Sprintf(`<td>%d</td>`, i+1))
        buf.WriteString(fmt.Sprintf(`<td><code>%s</code></td>`, html.EscapeString(item.TestPath)))
        buf.WriteString(fmt.Sprintf(`<td>%s <code>%s</code></td>`, html.EscapeString(item.TargetFunc), html.EscapeString(filepath.Base(item.TargetFile))))
        buf.WriteString(fmt.Sprintf(`<td>%s</td>`, html.EscapeString(item.Reason)))
        buf.WriteString(fmt.Sprintf(`<td><span class="badge badge-%s">%s</span></td>`, h.statusClass(item.Status), item.Status))
        buf.WriteString(`</tr>`)
    }

    buf.WriteString(`</tbody></table>`)
    return buf.String()
}

func (h *HTML) statusClass(status string) string {
    switch status {
    case "written":
        return "success"
    case "reverted":
        return "danger"
    case "skipped":
        return "warning"
    case "failed":
        return "danger"
    case "generated":
        return "info"
    default:
        return "secondary"
    }
}
```

**CSS Classes** (reuse existing `.badge-*`, add `.autotest-table` if needed).

### 8. JSON Format

**File**: `internal/report/report.go` (JSON output)

```go
func (r *Report) JSON() (string, error) {
    // If stats non-empty, merge autotest into top-level
    if r.data.Stats != nil {
        // Unmarshal stats into map
        var statsMap map[string]interface{}
        if err := json.Unmarshal(r.data.Stats, &statsMap); err != nil {
            return "", err
        }

        // Add autotest
        if r.data.AutoTest != nil {
            statsMap["autotest"] = r.data.AutoTest
        }

        return json.MarshalIndent(statsMap, "", "  ")
    }

    // Otherwise marshal full ReportData (AutoTest included via json tag)
    return json.MarshalIndent(r.data, "", "  ")
}
```

**Output Example**:

```json
{
  "autotest": {
    "items": [
      {
        "target_file": "internal/foo/bar.go",
        "target_func": "Bar",
        "reason": "0% covered",
        "test_path": "internal/foo/bar_test.go",
        "status": "written"
      }
    ],
    "before_coverage_pct": 62.0,
    "after_coverage_pct": 71.5
  },
  "stats": {
    "total_tests": 42,
    "passed": 40,
    "failed": 2
  }
}
```

**Empty Report**: No `autotest` key if `data.AutoTest == nil`.

## Error Handling

| Failure | Handling |
|---------|----------|
| Migration V005 already applied | `schema_migrations` idempotently skips |
| Unmarshal `autotest_report` fails | Log warning, `AutoTest` remains nil, report other sections render normally |
| `UpdateSessionAutoTest` fails | Log warning, don't fail run (best-effort) |
| Old DB without column | Migration runs at start of `cerberus report` and `cerberus run` (existing behavior) |

## Testing

### Unit Tests

**`internal/autotest/autotest_test.go`**:
- Existing 4 paths (dry-run, approve-deny, auto, revert) + failed path
- Assert `Items` slice: each item has correct `Status` per path
- Assert `Items` length matches expected gaps

**`internal/store/session_test.go`**:
- V005 migration test: column exists after migration
- `UpdateSessionAutoTest` write → `GetSession` read round-trip
- JSON marshal/unmarshal correctness

**`internal/report/markdown_test.go`**:
- `BuildReport` with `AutoTestReport` column → `AutoTest` non-nil
- Markdown output contains "AutoTest" heading
- Markdown output contains item table (substring assertion)
- Empty column → no AutoTest section

**`internal/report/html_test.go`**:
- Same structure as markdown tests
- HTML output contains `<h2>AutoTest</h2>`
- HTML output contains table with correct classes

**`internal/lifecycle/lifecycle_test.go`** (integration):
- Existing `autotest_integration_test` plus persistence assertion
- After Phase 4, query DB: `autotest_report` column non-empty
- Unmarshal and assert `Items` correctness

## Migration Path

1. **Existing users**: Migration V005 adds column with default empty string (no disruption)
2. **Existing runs**: `autotest_report` is empty (reports won't show AutoTest section)
3. **New runs**: AutoTest data persisted automatically

## Future Considerations

### JUnit XML Export (Optional)

If CI/CD interoperability becomes requirement, add `--format=junit`:

```bash
cerberus report --session-id=xxx --format=junit > junit.xml
```

This would:
- Generate JUnit XML from `AutoTestReport.Items`
- Map each item to a `<testcase>`:
  - `classname`: `TargetFile`
  - `name`: `TargetFunc`
  - `<failure>` if `Status == "failed"` or `Status == "reverted"`

**Not in current scope** - defer until explicit user request.

### Historical Trends (Future)

CircleCI-style insights: aggregate `autotest_report` across sessions to show:
- Coverage trend over time
- Flaky gaps (reverted → written in later runs)

**Requires**: New `autotest_history` table or aggregation queries.

## References

- Previous AutoTest design: [2026-06-14-autotest-coverage-design.md](./2026-06-14-autotest-coverage-design.md)
- JUnit XML schema: [www.ibm.com/docs/en/developer-for-zos/14.1.0?topic=formats-junit-xml-format](https://www.ibm.com/docs/en/developer-for-zos/14.1.0?topic=formats-junit-xml-format)
- GitHub Actions Job Summary: [docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#adding-a-job-summary](https://docs.github.com/en/actions/using-workflows/workflow-commands-for-github-actions#adding-a-job-summary)

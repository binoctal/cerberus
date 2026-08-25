# Browser UI Test Leg Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the rendered DOM a fourth observed surface — a `browser_expect` wait-type assertion, a `browser_flow` case type, a UI vocabulary with coverage-denominator assertions, run-level token injection, and dogfood wiring (vite preview).

**Architecture:** Extends the existing playwright-go `BrowserExecutor` (internal/head/agent/browser.go). Vocab assertions compile into deterministic `browser_flow` cases exactly as HTTP routes compile into sweep cases; coverage synthesizes one required edge per assertion and credits it from `browser_expect` evidence, mirroring the `http_request` pattern. Auth reuses the web-actor's `http_login` via `ResolveAuthHeader`.

**Tech Stack:** Go 1.25, playwright-go v0.5700.1 (already in go.mod), gopkg.in/yaml.v3, vitest-free (pure Go tests).

**Spec:** `cerberus-docs/superpowers/specs/2026-08-26-browser-ui-leg-design.md`

## Global Constraints

- Commit author `binoctal <binoctal@gmail.com>`, NO Co-Authored-By.
- Code comments and commit messages in English; follow existing comment density.
- No CGo; no new module dependencies (playwright-go already present).
- Docs only in `cerberus-docs/`. NEVER stage `apps/api/.dev.vars` (sibling repo).
- NEVER run wholesale `go test` in `../open-agents/bridge` (kiro). cerberus tests are safe.
- Selector grammar: Playwright engines only — `text=...`, `css=...`, `role=x[name=y]`.
- Step timeout ceiling 30s; default 10s.
- All unit tests runnable via `go test ./internal/... -run <Test>` (no live server needed unless named _live_).

---

### Task 1: `BrowserExpectAction` type + pure comparator evaluation

**Files:**
- Modify: `internal/types/actions_browser.go` (append)
- Modify: `internal/types/result_browser.go` (extend BrowserResult)
- Test: `internal/types/browser_expect_test.go` (create)

**Interfaces:**
- Consumes: existing `ActionBrowserGoto`-style constants in `internal/types` (find the ActionType const block; add `ActionBrowserExpect` alongside).
- Produces: `BrowserExpectAction{Selector, Expectation string, Timeout int}`; `EvaluateBrowserExpectation(comparator, observedText string, count int) (pass bool, observed string, err error)`; `BrowserResult` gains `Selector`, `Expectation`, `Observed string` (json `selector`, `expectation`, `observed`).

- [ ] **Step 1: Write the failing test**

```go
package types

import "testing"

func TestEvaluateBrowserExpectation(t *testing.T) {
	cases := []struct {
		name      string
		comparator string
		text      string
		count     int
		pass      bool
	}{
		{"text_present hit", "text_present", "Connected", 1, true},
		{"text_present miss", "text_present", "Connecting...", 1, false},
		{"text_absent clean", "text_absent", "", 0, true},
		{"text_absent violation", "text_absent", "Error: boom", 1, false},
		{"element_visible present", "element_visible", "", 1, true},
		{"element_visible absent", "element_visible", "", 0, false},
		{"count ok", "element_count>=2", "", 2, true},
		{"count under", "element_count>=2", "", 1, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pass, _, err := EvaluateBrowserExpectation(c.comparator, c.text, c.count)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pass != c.pass {
				t.Errorf("comparator %q text=%q count=%d: got pass=%v want %v", c.comparator, c.text, c.count, pass, c.pass)
			}
		})
	}
	t.Run("unknown comparator errors", func(t *testing.T) {
		if _, _, err := EvaluateBrowserExpectation("bogus", "", 0); err == nil {
			t.Error("expected error for unknown comparator")
		}
	})
}

func TestBrowserExpectActionValidate(t *testing.T) {
	if err := (BrowserExpectAction{Selector: "text=x", Expectation: "text_present"}).Validate(); err != nil {
		t.Errorf("valid action rejected: %v", err)
	}
	if err := (BrowserExpectAction{Expectation: "text_present"}).Validate(); err == nil {
		t.Error("empty selector must fail Validate")
	}
	if err := (BrowserExpectAction{Selector: "text=x"}).Validate(); err == nil {
		t.Error("empty expectation must fail Validate")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/types -run TestEvaluateBrowserExpectation -v`
Expected: FAIL — `undefined: EvaluateBrowserExpectation`, `undefined: BrowserExpectAction`.

- [ ] **Step 3: Write minimal implementation**

In `internal/types/actions_browser.go`, append:

```go
// BrowserExpectAction is a wait-type DOM assertion: poll the locator until it
// satisfies the comparator or the timeout expires. The one capability the
// goto/click/fill/eval quartet lacks — TextContent() does not wait, so async
// render makes instant checks flaky (spec 2026-08-26 §3.1).
type BrowserExpectAction struct {
	// Selector is a Playwright-engine selector (text=... | css=... | role=x[name=y]).
	Selector string `json:"selector"`
	// Expectation is the comparator: text_present | text_absent |
	// element_visible | element_count>=N.
	Expectation string `json:"expectation"`
	// Timeout is the wait window in seconds (default 10, hard cap 30).
	Timeout int `json:"timeout,omitempty"`
}

func (a BrowserExpectAction) GetActionType() ActionType { return ActionBrowserExpect }
func (a BrowserExpectAction) Target() string            { return a.Selector }
func (a BrowserExpectAction) Validate() error {
	if a.Selector == "" {
		return fmt.Errorf("selector is required")
	}
	if a.Expectation == "" {
		return fmt.Errorf("expectation comparator is required")
	}
	return nil
}
```

Add the const `ActionBrowserExpect ActionType = "browser_expect"` next to the other browser action constants (locate the block defining `ActionBrowserGoto`).

In `internal/types/result_browser.go`, extend `BrowserResult` (new fields, keep existing order) and add the pure evaluator:

```go
type BrowserResult struct {
	OK         bool          `json:"success"`
	URL        string        `json:"url"`
	Title      string        `json:"title"`
	Text       string        `json:"text,omitempty"`
	Screenshot string        `json:"screenshot,omitempty"` // base64 encoded
	EvalResult string        `json:"eval_result,omitempty"`
	// Assertion facts (browser_expect): expected vs observed, judged by the
	// executor; the Examiner reviews why, not whether.
	Selector    string        `json:"selector,omitempty"`
	Expectation string        `json:"expectation,omitempty"`
	Observed    string        `json:"observed,omitempty"`
	Latency     time.Duration `json:"duration"`
	Err         string        `json:"error,omitempty"`
}
```

```go
// EvaluateBrowserExpectation judges a comparator against one observation.
// Polarity (spec amendment A2): text_present passes on a hit within the
// window; text_absent passes only when the element NEVER appeared — the
// executor polls the whole window and fails fast on appearance. Pure; the
// executor supplies text ("", element not found) and the locator count.
func EvaluateBrowserExpectation(comparator, observedText string, count int) (bool, string, error) {
	switch comparator {
	case "text_present":
		return observedText != "", observedText, nil
	case "text_absent":
		return observedText == "", observedText, nil
	case "element_visible":
		return count > 0, "", nil
	default:
		if strings.HasPrefix(comparator, "element_count>=") {
			n, err := strconv.Atoi(strings.TrimPrefix(comparator, "element_count>="))
			if err != nil || n < 0 {
				return false, "", fmt.Errorf("bad count comparator %q", comparator)
			}
			return count >= n, fmt.Sprintf("%d", count), nil
		}
		return false, "", fmt.Errorf("unknown comparator %q", comparator)
	}
}
```

(Add `"strconv"` and `"strings"` imports if absent.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/types -run 'TestEvaluateBrowserExpectation|TestBrowserExpectActionValidate' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/types/actions_browser.go internal/types/result_browser.go internal/types/browser_expect_test.go
git commit -m "feat(types): browser_expect wait-type DOM assertion action + pure comparator evaluation"
```

---

### Task 2: Executor — expect assertion + screenshot-to-file

**Files:**
- Modify: `internal/head/agent/browser.go`
- Test: `internal/head/agent/browser_expect_executor_test.go` (create)

**Interfaces:**
- Consumes: `types.BrowserExpectAction`, `types.EvaluateBrowserExpectation` (Task 1).
- Produces: `(e *BrowserExecutor) expectAssertion(a types.BrowserExpectAction, start time.Time) types.ExecutorResult`; `(e *BrowserExecutor) ScreenshotToFile(caseID, label string) (string, error)` writing `<projectDir>/.cerberus/runtime/shots/{caseID}-{label}.png`; `NewBrowserExecutor` gains the project dir as first arg: `NewBrowserExecutor(projectDir string, logger *zap.Logger)`.
- Poll loop: 250 ms interval, window = `a.Timeout` seconds clamped `[1,30]`, default 10.

- [ ] **Step 1: Write the failing test (poll-window math + file naming, pure parts)**

The Playwright page is not unit-mockable here (the executor holds concrete `pw.Page`); test the pure seams: timeout clamping and the shot filename. Live behavior is covered by the Task 10 dogfood run.

```go
package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestBrowserExpectWindowClamp(t *testing.T) {
	for _, c := range []struct{ in, want int }{{0, 10}, {-3, 10}, {15, 15}, {99, 30}} {
		if got := expectWindowSeconds(c.in); got != c.want {
			t.Errorf("expectWindowSeconds(%d)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestShotPath(t *testing.T) {
	got := shotPath("/proj", "case-1", "after-create")
	want := filepath.Join("/proj", ".cerberus", "runtime", "shots", "case-1-after-create.png")
	if got != want {
		t.Errorf("shotPath=%q want %q", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent -run 'TestBrowserExpectWindowClamp|TestShotPath' -v`
Expected: FAIL — undefined helpers.

- [ ] **Step 3: Write minimal implementation**

In `internal/head/agent/browser.go`:

```go
// expectWindowSeconds clamps a declared step timeout to [1,30] with default
// 10 (spec §8: 30 s hard cap keeps a hung page from stalling the sweep).
func expectWindowSeconds(declared int) int {
	if declared <= 0 {
		return 10
	}
	if declared > 30 {
		return 30
	}
	return declared
}

func shotPath(projectDir, caseID, label string) string {
	return filepath.Join(projectDir, ".cerberus", "runtime", "shots", caseID+"-"+label+".png")
}
```

Change `NewBrowserExecutor(logger)` → `NewBrowserExecutor(projectDir string, logger *zap.Logger)`, store `projectDir`, and update the single caller in `plugin_helpers.go` (`BuiltinExecutorPlugins` already receives `projectDir`). Update `browser_test.go` call sites accordingly.

Add to `BrowserExecutor.Execute`'s switch:

```go
case types.BrowserExpectAction:
	return e.expectAssertion(a, start)
```

```go
// expectAssertion polls the locator until the comparator holds or the window
// expires. Polarity A2: text_absent fails FAST on appearance and passes only
// by outliving the window; every other comparator passes on first hit.
func (e *BrowserExecutor) expectAssertion(a types.BrowserExpectAction, start time.Time) types.ExecutorResult {
	window := time.Duration(expectWindowSeconds(a.Timeout)) * time.Second
	deadline := start.Add(window)
	locator := e.page.Locator(a.Selector)
	for {
		text, _ := locator.TextContent()
		count, _ := locator.Count()
		observed := ""
		if text != nil {
			observed = strings.TrimSpace(*text)
		}
		pass, obs, err := types.EvaluateBrowserExpectation(a.Expectation, observed, count)
		if err != nil {
			return types.BrowserResult{OK: false, URL: e.page.URL(), Selector: a.Selector,
				Expectation: a.Expectation, Err: err.Error(), Latency: time.Since(start)}
		}
		if pass || (a.Expectation != "text_absent" && time.Now().After(deadline)) {
			return types.BrowserResult{OK: pass, URL: e.page.URL(), Selector: a.Selector,
				Expectation: a.Expectation, Observed: obs, Latency: time.Since(start),
				Err: failReason(a, pass, obs)}
		}
		// text_absent: appearance already returned pass=false above; only the
		// deadline ends a surviving wait.
		if time.Now().After(deadline) {
			return types.BrowserResult{OK: pass, URL: e.page.URL(), Selector: a.Selector,
				Expectation: a.Expectation, Observed: obs, Latency: time.Since(start),
				Err: failReason(a, pass, obs)}
		}
		select {
		case <-e.ctxDone(): // executor ctx cancellation, if wired; else time.Sleep
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// failReason renders "" on pass so Err stays empty for successes.
func failReason(a types.BrowserExpectAction, pass bool, observed string) string {
	if pass {
		return ""
	}
	return fmt.Sprintf("expect %s %q: not satisfied within window (observed %q)", a.Expectation, a.Selector, observed)
}
```

NOTE on `e.ctxDone()`: if wiring executor context proves invasive, replace the select with a plain `time.Sleep(250 * time.Millisecond)` and keep the comment explaining why (the per-case timeout in `executeStep` bounds the loop anyway). Prefer the simple sleep.

`ScreenshotToFile` (spec §6: evidence frame stores the path, not base64):

```go
// ScreenshotToFile captures the page to the run's shots dir and returns the
// path. Called by browser_shot steps and auto-captured on any step failure.
func (e *BrowserExecutor) ScreenshotToFile(caseID, label string) (string, error) {
	data, err := e.page.Screenshot(pw.PageScreenshotOptions{Type: pw.ScreenshotTypePng})
	if err != nil {
		return "", err
	}
	p := shotPath(e.projectDir, caseID, label)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return p, os.WriteFile(p, data, 0o644)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/head/agent -run 'TestBrowserExpectWindowClamp|TestShotPath' -v && go build ./...`
Expected: PASS + clean build (including the `plugin_helpers.go` caller update).

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/browser.go internal/head/agent/plugin_helpers.go internal/head/agent/browser_expect_executor_test.go internal/head/agent/browser_test.go
git commit -m "feat(agent): browser_expect polling assertion + screenshot-to-file in BrowserExecutor"
```

---

### Task 3: Rule mapping for the atomic `browser_expect` case

**Files:**
- Modify: `internal/head/agent/rules_browser.go`
- Test: `internal/head/agent/rules_browser_test.go` (create or extend)

**Interfaces:**
- Consumes: `types.BrowserExpectAction` (Task 1), existing `matchBrowserRules` switch.
- Produces: rule-engine recognition of `Action: browser_expect` with comparator parsed from `tc.Expectation` (format `"<comparator>"`, e.g. `text_present`; timeout from `tc.Timeout` if the TestCase carries one, else 0 → default).

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

func TestMatchBrowserRulesExpect(t *testing.T) {
	re := &RuleEngine{}
	tc := TestCase{Action: "browser_expect", Target: "text=Connected", Expectation: "text_present"}
	act, ok := re.matchBrowserRules(tc)
	if !ok {
		t.Fatal("browser_expect not matched")
	}
	be, is := act.(types.BrowserExpectAction)
	if !is {
		t.Fatalf("got %T want BrowserExpectAction", act)
	}
	if be.Selector != "text=Connected" || be.Expectation != "text_present" {
		t.Errorf("mapping wrong: %+v", be)
	}
}
```

(If `RuleEngine` zero value is not constructible in tests, mirror the construction used by `rules_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent -run TestMatchBrowserRulesExpect -v`
Expected: FAIL — not matched.

- [ ] **Step 3: Write minimal implementation**

In `matchBrowserRules`, before `default:`:

```go
	// Rule 15b: browser_expect — wait-type DOM assertion. The comparator
	// rides Expectation; timeout defaults inside the executor.
	case "browser_expect":
		return types.BrowserExpectAction{Selector: tc.Target, Expectation: tc.Expectation, Timeout: tc.Timeout}, true
```

(Verify `TestCase.Timeout` exists and its unit is seconds; if `TestCase` has no Timeout field, pass `0` and rely on the executor default.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/head/agent -run TestMatchBrowserRulesExpect -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/rules_browser.go internal/head/agent/rules_browser_test.go
git commit -m "feat(agent): rule-engine mapping for atomic browser_expect cases"
```

---

### Task 4: `browser_flow` step dispatch in the deterministic Steps runner

**Files:**
- Modify: `internal/head/agent/execute_phases_steps.go`
- Test: `internal/head/agent/browser_flow_steps_test.go` (create)

**Interfaces:**
- Consumes: `runSteps` loop (execute_phases_steps.go:353), `stepToAction` (line ~192), `stepEvidence` (line ~54), typed actions from Tasks 1-2.
- Produces: step verbs `browser_goto`, `browser_click`, `browser_fill`, `browser_expect`, `browser_shot` inside any case with `Steps`; evidence entries with `Action: "browser_expect"`, `MatchedType: <s.Type>`, `Matched: <pass>`; `resolveBrowserStep(tc *TestCase, s TestStep) (types.TypedAction, error)`.

- [ ] **Step 1: Write the failing test**

```go
package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/types"
)

func TestResolveBrowserStep(t *testing.T) {
	tc := &TestCase{Target: "http://localhost:5183"}

	got, err := resolveBrowserStep(tc, TestStep{Action: "browser_goto", URL: "/dashboard/missions"})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(types.BrowserGotoAction); !ok || g.URL != "http://localhost:5183/dashboard/missions" {
		t.Errorf("goto: got %+v", got)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_goto", URL: "http://other:1/x"})
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := got.(types.BrowserGotoAction); !ok || g.URL != "http://other:1/x" {
		t.Errorf("absolute URL must win: %+v", got)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_click", Target: "text=Run"})
	if g, ok := got.(types.BrowserClickAction); err != nil || !ok || g.Selector != "text=Run" {
		t.Errorf("click: got %+v err %v", got, err)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_fill", Target: "css=input", Message: "hello"})
	if g, ok := got.(types.BrowserFillAction); err != nil || !ok || g.Value != "hello" {
		t.Errorf("fill: got %+v err %v", got, err)
	}

	got, err = resolveBrowserStep(tc, TestStep{Action: "browser_expect", Target: "text=Connected", Type: "missions-conn-status", Asserts: map[string]any{"expectation": "text_present"}, Timeout: 15})
	if err != nil {
		t.Fatal(err)
	}
	be, ok := got.(types.BrowserExpectAction)
	if !ok || be.Selector != "text=Connected" || be.Expectation != "text_present" || be.Timeout != 15 {
		t.Errorf("expect: got %+v", got)
	}

	if _, err = resolveBrowserStep(tc, TestStep{Action: "browser_nope"}); err == nil {
		t.Error("unknown verb must error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/agent -run TestResolveBrowserStep -v`
Expected: FAIL — undefined `resolveBrowserStep`.

- [ ] **Step 3: Write minimal implementation**

In `execute_phases_steps.go`, add:

```go
// browserExpectComparator reads the comparator off a browser_expect step:
// s.Asserts["expectation"] when the step came from YAML/vocab, else
// "text_present" (the overwhelmingly common shape).
func browserExpectComparator(s TestStep) string {
	if v, ok := s.Asserts["expectation"].(string); ok && v != "" {
		return v
	}
	return "text_present"
}

// resolveBrowserStep turns a browser_* TestStep into its typed action. URL
// resolution mirrors ws_connect: an absolute s.URL wins; otherwise it is a
// route joined onto tc.Target (the UI base URL carried by the case).
func resolveBrowserStep(tc *TestCase, s TestStep) (types.TypedAction, error) {
	switch s.Action {
	case "browser_goto":
		url := s.URL
		if url == "" || !isURL(url) {
			url = strings.TrimSuffix(tc.Target, "/") + "/" + strings.TrimPrefix(url, "/")
		}
		return types.BrowserGotoAction{URL: url}, nil
	case "browser_click":
		return types.BrowserClickAction{Selector: s.Target}, nil
	case "browser_fill":
		return types.BrowserFillAction{Selector: s.Target, Value: s.Message}, nil
	case "browser_expect":
		return types.BrowserExpectAction{Selector: s.Target,
			Expectation: browserExpectComparator(s), Timeout: s.Timeout}, nil
	default:
		return nil, fmt.Errorf("browser steps: unknown action %q", s.Action)
	}
}
```

In `runSteps`, extend the dispatch (`if s.Action == "http_request" {...} else if strings.HasPrefix(s.Action, "browser_") { action, err = resolveBrowserStep(se.tc, s) } else {...}`), and after `r.executor.Execute` add a browser branch before the generic `!result.Success()` check:

```go
		if strings.HasPrefix(s.Action, "browser_") {
			if s.Action == "browser_shot" {
				// Not a typed executor action: capture via the executor's
				// file sink; the Evidence entry carries the path.
				if be := r.browserExec(); be != nil {
					caseID, _ := se.ctx.Value(caseIDKey{}).(string)
					if p, serr := be.ScreenshotToFile(caseID, s.Label); serr == nil {
						evidence = append(evidence, Evidence{Type: "browser_shot",
							Content: fmt.Sprintf("browser_shot: %s", p), Action: s.Action})
					}
				}
				continue
			}
			// Auto-capture on failure: the DOM excerpt rides the result; the
			// screenshot rides the shots dir.
			if !result.Success() {
				if be := r.browserExec(); be != nil {
					caseID, _ := se.ctx.Value(caseIDKey{}).(string)
					_, _ = be.ScreenshotToFile(caseID, "fail")
				}
				return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
					Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result,
					Evidence: evidence, Error: fmt.Errorf("%s", result.Summary())}
			}
			continue
		}
```

`r.browserExec()` — add a tiny accessor on ReActLoop that walks the plugin registry for the browser plugin and returns `*BrowserExecutor` (nil when absent), cached in a field; follow how the loop reaches other executors. Add `Label string` to `TestStep` if absent (`yaml:"label,omitempty"`), and `caseIDKey` is already defined in this package.

In `stepEvidence`, add before the ws_receive branch:

```go
	if s.Action == "browser_expect" {
		ev.MatchedType = s.Type // the assertion id (vocab) — coverage credits on it
		if br, ok := result.(types.BrowserResult); ok {
			ev.Matched = br.OK
			ev.Content = fmt.Sprintf("browser_expect %s: %s", s.Type, result.Summary())
		}
	}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/head/agent -run 'TestResolveBrowserStep|TestStepEvidence' -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/execute_phases_steps.go internal/head/agent/browser_flow_steps_test.go internal/head/agent/types.go
git commit -m "feat(agent): browser_flow step verbs in the deterministic Steps runner + assertion evidence"
```

---

### Task 5: UI vocabulary schema + validation

**Files:**
- Modify: `internal/project/vocabulary.go`
- Test: `internal/project/vocabulary_ui_test.go` (create)

**Interfaces:**
- Consumes: `Vocabulary` struct (vocabulary.go:16).
- Produces: `VocabUI{BaseURL string \`yaml:"base_url"\`, Locale string \`yaml:"locale"\`, AuthActor string \`yaml:"auth_actor,omitempty"\`, Assertions []VocabUIAssertion \`yaml:"assertions"\`}`; `VocabUIAssertion{ID, Route, Target, Expectation string, Timeout int, Unsupported bool, Reason string}` (yaml: `id,route,target,expectation,timeout,unsupported,reason`); `Vocabulary.UI *VocabUI \`yaml:"ui,omitempty"\``; validation in `Vocabulary.Validate()` — non-nil UI requires BaseURL, Locale, and every non-unsupported assertion requires ID/Route/Target/Expectation with a known comparator (`text_present|text_absent|element_visible|element_count>=N`); unique IDs.

- [ ] **Step 1: Write the failing test**

```go
package project

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVocabularyUIDecodeAndValidate(t *testing.T) {
	src := `
source: {protocol_ref: ""}
edges: []
ui:
  base_url: http://localhost:5183
  locale: en
  assertions:
    - id: missions-conn-status
      route: /dashboard/missions
      target: "text=Connected"
      expectation: text_present
      timeout: 15
    - id: devices-counter
      route: /dashboard/devices
      target: "text=devices online"
      expectation: text_present
    - id: exempt-page
      route: /dashboard/plan
      target: "css=.billing"
      expectation: element_visible
      unsupported: true
      reason: plan-gated render depends on the seeded plan row
`
	var v Vocabulary
	if err := yaml.Unmarshal([]byte(src), &v); err != nil {
		t.Fatal(err)
	}
	if v.UI == nil || len(v.UI.Assertions) != 3 {
		t.Fatalf("decode: %+v", v.UI)
	}
	if err := v.Validate(); err != nil {
		t.Fatalf("valid ui vocab rejected: %v", err)
	}

	// Missing base_url / locale / assertion fields / dup ids / bad comparator.
	bad := []string{
		"ui:\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present}]",
		"ui:\n  base_url: http://x\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present}]",
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, target: \"text=x\", expectation: text_present}]",
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: wat}]",
		"ui:\n  base_url: http://x\n  locale: en\n  assertions: [{id: a, route: /r, target: \"text=x\", expectation: text_present},{id: a, route: /r, target: \"text=y\", expectation: text_present}]",
	}
	for i, src := range bad {
		var v Vocabulary
		if err := yaml.Unmarshal([]byte(src), &v); err != nil {
			t.Fatalf("case %d unmarshal: %v", i, err)
		}
		if err := v.Validate(); err == nil {
			t.Errorf("case %d: invalid ui vocab accepted", i)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/project -run TestVocabularyUIDecodeAndValidate -v`
Expected: FAIL — unknown field `ui` ignored (v.UI nil) or Validate missing checks.

- [ ] **Step 3: Write minimal implementation**

Add the types and a `validateUI()` method called from `Vocabulary.Validate()` (mirroring how HTTPRoutes are validated around vocabulary.go:113). Comparator check:

```go
func uiComparatorKnown(c string) bool {
	if c == "text_present" || c == "text_absent" || c == "element_visible" {
		return true
	}
	return strings.HasPrefix(c, "element_count>=")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/project -run TestVocabularyUIDecodeAndValidate -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/project/vocabulary.go internal/project/vocabulary_ui_test.go
git commit -m "feat(project): ui vocabulary section — base_url/locale/auth_actor + coverage-denominator assertions"
```

---

### Task 6: `uiVocabCases` generator + aggregator wiring

**Files:**
- Create: `internal/head/scout/ui_cases.go`
- Modify: `internal/head/scout/ws_cases.go` (both aggregator sites, lines ~52 and ~384, next to `httpRouteCases(svc)`)
- Test: `internal/head/scout/ui_cases_test.go` (create)

**Interfaces:**
- Consumes: `project.Service` with `Vocabulary.UI` (Task 5), `agent.TestCase`/`TestStep`.
- Produces: `uiVocabCases(svc project.Service) []agent.TestCase` — one case per non-unsupported assertion: `ID: "ui-vocab-" + a.ID`, `Action: "browser_flow"`, `Target: base_url`, `Service: svc.Name`, `Steps: [{Action:"browser_goto", URL:a.Route},{Action:"browser_expect", Target:a.Target, Type:a.ID, Asserts:{"expectation":a.Expectation}, Timeout:a.Timeout}]`, `Expectation: "UI assertion " + a.ID + " holds (" + a.Expectation + " " + a.Target + ")"`.

- [ ] **Step 1: Write the failing test**

```go
package scout

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestUIVocabCases(t *testing.T) {
	svc := project.Service{Name: "open-agents", URL: "ws://localhost:8989/ws"}
	svc.Vocabulary = &project.Vocabulary{UI: &project.VocabUI{
		BaseURL: "http://localhost:5183", Locale: "en",
		Assertions: []project.VocabUIAssertion{
			{ID: "missions-conn-status", Route: "/dashboard/missions", Target: "text=Connected", Expectation: "text_present", Timeout: 15},
			{ID: "exempt", Route: "/x", Target: "text=y", Expectation: "text_present", Unsupported: true},
		},
	}}
	cases := uiVocabCases(svc)
	if len(cases) != 1 {
		t.Fatalf("want 1 case (unsupported skipped), got %d", len(cases))
	}
	c := cases[0]
	if c.ID != "ui-vocab-missions-conn-status" || c.Action != "browser_flow" || c.Target != "http://localhost:5183" {
		t.Fatalf("case shape: %+v", c)
	}
	if len(c.Steps) != 2 || c.Steps[0].Action != "browser_goto" || c.Steps[0].URL != "/dashboard/missions" {
		t.Fatalf("steps: %+v", c.Steps)
	}
	e := c.Steps[1]
	if e.Action != "browser_expect" || e.Target != "text=Connected" || e.Type != "missions-conn-status" || e.Timeout != 15 {
		t.Fatalf("expect step: %+v", e)
	}
	if e.Asserts["expectation"] != "text_present" {
		t.Fatalf("comparator: %+v", e.Asserts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/head/scout -run TestUIVocabCases -v`
Expected: FAIL — undefined `uiVocabCases`.

- [ ] **Step 3: Write minimal implementation**

`internal/head/scout/ui_cases.go` (comment header mirroring `http_route_cases.go`'s honesty-tier note):

```go
package scout

import (
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// uiVocabCases emits one deterministic browser_flow per declared UI
// assertion: goto the route, wait-assert the display promise (spec §4).
// Assertions are STATIC promises — true of the page itself once rendered;
// protocol-coupled values (mission card task counts) are a follow-up.
// Not claim-bound (same honesty tier as httpRouteCases: display reachability,
// not ledger promises). unsupported assertions are outside the denominator.
func uiVocabCases(svc project.Service) []agent.TestCase {
	if svc.Vocabulary == nil || svc.Vocabulary.UI == nil {
		return nil
	}
	ui := svc.Vocabulary.UI
	var cases []agent.TestCase
	for _, a := range ui.Assertions {
		if a.Unsupported {
			continue
		}
		timeout := a.Timeout
		if timeout == 0 {
			timeout = 10
		}
		cases = append(cases, agent.TestCase{
			ID:      "ui-vocab-" + a.ID,
			Name:    "UI assertion " + a.ID,
			Action:  "browser_flow",
			Target:  ui.BaseURL,
			Service: svc.Name,
			Steps: []agent.TestStep{
				{Action: "browser_goto", URL: a.Route},
				{Action: "browser_expect", Target: a.Target, Type: a.ID,
					Asserts: map[string]any{"expectation": a.Expectation}, Timeout: timeout},
			},
			Expectation: "UI assertion " + a.ID + " holds (" + a.Expectation + " " + a.Target + ")",
		})
	}
	return cases
}
```

Wire into both aggregator sites in `ws_cases.go`: `cases = append(cases, uiVocabCases(svc)...)` immediately after each `httpRouteCases(svc)` call.

- [ ] **Step 4: Run test + neighbors**

Run: `go test ./internal/head/scout -run 'TestUIVocabCases|TestWSCases|TestPlanCases' -v`
Expected: PASS (existing aggregators unaffected — verify with the scout suite: `go test ./internal/head/scout`).

- [ ] **Step 5: Commit**

```bash
git add internal/head/scout/ui_cases.go internal/head/scout/ui_cases_test.go internal/head/scout/ws_cases.go
git commit -m "feat(scout): uiVocabCases — deterministic browser_flow sweep from the ui vocabulary"
```

---

### Task 7: Run-level browser session + token injection

**Files:**
- Modify: `internal/head/agent/browser.go` (session init)
- Modify: `internal/head/agent/plugin_helpers.go` (wire init after registration)
- Test: `internal/head/agent/browser_session_test.go` (create)

**Interfaces:**
- Consumes: `ResolveAuthHeader(ctx, svcURL, actor)` (authflow.go:151) with the actor named by `ui.AuthActor` (default `"web-actor"`), `project.LoadFromYAML` or equivalent for reading the project config from `projectDir`.
- Produces: `(e *BrowserExecutor) InitSession(ctx context.Context, baseURL, locale string, auth *AuthResult, userID string) error` — goto baseURL bare (origin bootstrap), then evaluate the zustand persist blob:

```go
// InitSession establishes the run-level logged-in browser session: one bare
// goto bootstraps the origin, then the auth-storage blob (zustand persist
// shape the web app hydrates from) + the i18n locale key are written once.
// Pages opened later share the browser context and thus the localStorage.
func (e *BrowserExecutor) InitSession(ctx context.Context, baseURL, locale string, auth *AuthResult, userID string) error {
	if _, err := e.page.Goto(baseURL); err != nil {
		return fmt.Errorf("session bootstrap goto: %w", err)
	}
	blob := map[string]any{
		"state": map[string]any{
			"user":          map[string]any{"id": userID, "email": "dev@openagents.local", "name": "Dev User"},
			"token":         auth.Token,
			"refreshToken":  auth.RefreshToken,
			"expiresIn":     900,
			"tokenExpiry":   time.Now().Add(15 * time.Minute).UnixMilli(),
		},
		"version": 0,
	}
	b, _ := json.Marshal(blob)
	_, err := e.page.Evaluate(fmt.Sprintf(
		`localStorage.setItem('auth-storage', %q); localStorage.setItem('i18nextLng', %q); undefined`, string(b), locale))
	return err
}
```

(`AuthResult` carrying refreshToken: check `authflow.go` — if it only carries Token, extend the blob to omit refreshToken; the WS provider only needs `token` for the initial connect. Pin the actual field names during implementation against `apps/web/src/stores/authStore.ts` partialize — that file is the contract.)

Wiring in `BuiltinPluginsWithSandbox` (plugin_helpers.go), right after the browser plugin registers: load the project config from `projectDir`; for the first service whose `Vocabulary.UI != nil`, resolve the actor named `ui.AuthActor` (or `web-actor`), call `ResolveAuthHeader(ctx, svcHTTPURL, actor)` (svc HTTP URL = the service URL with ws→http), then `browserExec.InitSession(...)`. Failures log a warning and skip injection (unauthenticated assertions still run; authed pages will fail their assertions loudly — spec §8 keeps the UI leg alive).

- [ ] **Step 1: Write the failing test** — blob shape is the contract; test the marshal helper:

```go
package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAuthStorageBlob(t *testing.T) {
	b := authStorageBlob("tok", "user_1", time.UnixMilli(1787675385090))
	var m map[string]any
	if err := json.Unmarshal([]byte(b), &m); err != nil {
		t.Fatal(err)
	}
	st := m["state"].(map[string]any)
	if st["token"] != "tok" {
		t.Errorf("token: %v", st["token"])
	}
	u := st["user"].(map[string]any)
	if u["id"] != "user_1" {
		t.Errorf("user id: %v", u["id"])
	}
	if !strings.Contains(b, `"version":0`) {
		t.Error("zustand persist version field missing")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/head/agent -run TestAuthStorageBlob -v`
Expected: FAIL — undefined `authStorageBlob`.

- [ ] **Step 3: Implement** `authStorageBlob(token, userID string, expiryMs int64) string` (pure marshal; `InitSession` uses it) + the `InitSession` method + the plugin_helpers wiring as described above.

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/head/agent -run 'TestAuthStorageBlob' -v && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/head/agent/browser.go internal/head/agent/plugin_helpers.go internal/head/agent/browser_session_test.go
git commit -m "feat(agent): run-level browser session — web-actor JWT + locale injected into localStorage once per run"
```

---

### Task 8: Coverage denominator + attribution + repair exclusion

**Files:**
- Modify: `internal/session/coverage.go` (requiredEdges ~line 355, exercisedEdges ~line 239)
- Modify: `internal/session/run_phases_repair.go` (isRepairable ~line 322)
- Test: `internal/session/coverage_ui_test.go` (create), `internal/session/repair_browser_flow_test.go` (create)

**Interfaces:**
- Consumes: `project.VocabUIAssertion` (Task 5), evidence `Action == "browser_expect"` with `MatchedType` = assertion id (Task 4).
- Produces: per assertion a synthesized edge `VocabEdge{FromRole: "browser", ToRole: "web_ui", Type: "ui_assert " + a.ID, Trigger: "ui_assert"}`; attribution rule crediting on matched browser_expect evidence; `isRepairable` returns false for `tc.Action == "browser_flow"`.

- [ ] **Step 1: Write the failing tests**

```go
package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func uiSvc() *project.Config { /* build a Config with one service whose Vocabulary.UI has 2 assertions, one unsupported */ }

func TestRequiredEdgesIncludesUIAssertions(t *testing.T) {
	sess := &Session{Config: uiSvc()}
	edges := requiredEdges(sess)
	var uiCount int
	for _, e := range edges {
		if e.Trigger == "ui_assert" {
			uiCount++
		}
	}
	if uiCount != 1 { // unsupported assertion excluded
		t.Fatalf("ui edges: got %d want 1 (%+v)", uiCount, edges)
	}
}

func TestExercisedEdgesCreditsBrowserExpect(t *testing.T) {
	sess := &Session{Config: uiSvc()}
	req := requiredEdges(sess)
	results := []agent.StepResult{{
		TestCase: &agent.TestCase{Action: "browser_flow", Target: "http://localhost:5183", Steps: []agent.TestStep{
			{Action: "browser_goto", URL: "/dashboard/missions"},
			{Action: "browser_expect", Type: "missions-conn-status"},
		}},
		Evidence: []agent.Evidence{
			{Action: "browser_expect", MatchedType: "missions-conn-status", Matched: true},
		},
	}}
	exercised, _ := exercisedEdges(results, req, nil)
	for _, e := range req {
		if e.Trigger == "ui_assert" && !exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] {
			t.Errorf("ui_assert edge %s not credited", e.Type)
		}
	}
}
```

```go
package session

import (
	"testing"

	"github.com/binoctal/cerberus/internal/agent"
)

func TestBrowserFlowNotRepairable(t *testing.T) {
	tc := agent.TestCase{Action: "browser_flow", Target: "http://localhost:5183",
		Steps: []agent.TestStep{{Action: "browser_goto"}}}
	if isRepairable(&tc) {
		t.Error("browser_flow must not be repairable — repair_case emits HTTP/WS shapes only")
	}
}
```

(Adapt `uiSvc()` construction to the real `project.Config`/`Service` literals used by existing coverage tests — see `coverage_test.go` for the shape.)

- [ ] **Step 2: Run to verify both fail**

Run: `go test ./internal/session -run 'TestRequiredEdgesIncludesUIAssertions|TestExercisedEdgesCreditsBrowserExpect|TestBrowserFlowNotRepairable' -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `requiredEdges`, after the HTTPRoutes block:

```go
		// UI display promises: one required edge per declared assertion
		// (unsupported excluded), same synthesis pattern as HTTP routes.
		// FromRole "browser" / ToRole "web_ui" are reserved names that no
		// WS role can collide with; Trigger "ui_assert" distinguishes them.
		if svc.Vocabulary != nil && svc.Vocabulary.UI != nil {
			for _, a := range svc.Vocabulary.UI.Assertions {
				if a.Unsupported {
					continue
				}
				out = append(out, project.VocabEdge{
					FromRole: "browser",
					ToRole:   "web_ui",
					Type:     "ui_assert " + a.ID,
					Trigger:  "ui_assert",
				})
			}
		}
```

In `exercisedEdges`, next to the `http_request` evidence branch:

```go
			// browser_expect evidence credits the matching ui_assert edge:
			// MatchedType carries the assertion id (vocab cases set it).
			if ev.Action == "browser_expect" && ev.Matched && ev.MatchedType != "" {
				for _, e := range required {
					if e.Trigger == "ui_assert" && e.Type == "ui_assert "+ev.MatchedType {
						exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] = true
					}
				}
				continue
			}
```

In `isRepairable`:

```go
	if tc.Action == "browser_flow" {
		return false // browser steps have no repair_case shape
	}
```

- [ ] **Step 4: Run the session suite**

Run: `go test ./internal/session -run 'UIAssertions|BrowserExpect|BrowserFlowNotRepairable' -v && go test ./internal/session`
Expected: PASS, full suite green.

- [ ] **Step 5: Commit**

```bash
git add internal/session/coverage.go internal/session/run_phases_repair.go internal/session/coverage_ui_test.go internal/session/repair_browser_flow_test.go
git commit -m "feat(session): ui_assert edges in the coverage denominator, browser_expect attribution, browser_flow repair exclusion"
```

---

### Task 9: Dogfood config + vite preview env

**Files:**
- Modify: `dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml` (append ui section)
- Modify: `scripts/dogfood-realtime-e2e-env.sh` (start web preview)
- Modify: `dogfood/realtime-e2e/.cerberus/project.yaml` (web-actor needs `http_login` — ALREADY PRESENT, verify only; add `ui.auth_actor: web-actor` is default so no change)

**Interfaces:**
- Consumes: vocab schema (Task 5), env script pattern from `scripts/integration-openagents.sh` (fnm node-22 guard + process-group kill).

- [ ] **Step 1: Append the ui section to the vocab yaml**

```yaml
ui:
  base_url: http://localhost:5183
  locale: en
  auth_actor: web-actor
  assertions:
    - id: missions-conn-status
      route: /dashboard/missions
      target: "text=Connected"
      expectation: text_present
      timeout: 15
    - id: missions-device-counter
      route: /dashboard/missions
      target: "text=devices online"
      expectation: text_present
      timeout: 15
    - id: missions-list-renders
      route: /dashboard/missions
      target: "css=aside"
      expectation: element_visible
    - id: devices-page-populated
      route: /dashboard/devices
      target: "css=table tbody tr"
      expectation: element_count>=1
      timeout: 15
```

- [ ] **Step 2: Verify decode**

Run: `cd dogfood/realtime-e2e && ../../build/cerberus validate 2>&1 | head -5` (or the repo's equivalent config-check command — check `config_test.go` for how the dogfood config is validated; adjust the command to whatever exists, e.g. `go test ./dogfood/realtime-e2e -run TestConfig`).
Expected: no validation errors naming the ui section.

- [ ] **Step 3: Add vite preview to the env script**

Append to `scripts/dogfood-realtime-e2e-env.sh` (mirroring the bridge-build block's tone; preview not dev — no file watchers, deterministic, spec §7):

```bash
# Web UI for the browser leg (spec 2026-08-26 §7): build once, serve the
# static bundle via vite preview. NOT `vite dev` — dev's file watchers hit
# inotify ENOSPC on this host and die mid-run (observed 2026-08-25).
_WEB_DIR="${REPO_ROOT}/../open-agents/apps/web"
if [ -d "${_WEB_DIR}" ] && command -v fnm >/dev/null 2>&1; then
  echo "building web bundle (vite build)"
  (cd "${_WEB_DIR}" && eval "$(fnm env)" && fnm use 22 >/dev/null 2>&1 && npm run build --silent) \
    || echo "WARNING: web build failed - UI vocab cases will fail target_unreachable"
  echo "starting vite preview :5183"
  (cd "${_WEB_DIR}" && eval "$(fnm env)" && fnm use 22 >/dev/null 2>&1 && exec npx vite preview --port 5183 --strictPort) &
  UI_PREVIEW_PID=$!
  trap 'kill -- -${UI_PREVIEW_PID} 2>/dev/null' EXIT
  export CERBERUS_UI_PREVIEW_PID="${UI_PREVIEW_PID}"
fi
```

(If the script is sourced rather than exec'd, a trap on EXIT in a sourced context fires when the sourcing shell exits — acceptable for the manual dogfood flow; note it in a comment. Verify `npm run preview` vs `npx vite preview` against apps/web/package.json scripts and use the package-script form if present.)

- [ ] **Step 4: Smoke the preview**

Run: `bash -c 'source scripts/dogfood-realtime-e2e-env.sh; sleep 8; curl -s -o /dev/null -w "%{http_code}\n" http://localhost:5183; kill ${CERBERUS_UI_PREVIEW_PID}'`
Expected: `200`.

- [ ] **Step 5: Commit**

```bash
git add dogfood/realtime-e2e/.cerberus/vocab/open-agents.vocab.yaml scripts/dogfood-realtime-e2e-env.sh
git commit -m "feat(dogfood): ui vocabulary assertions + vite preview env for the browser leg"
```

---

### Task 10: Live dogfood validation

**Files:**
- Create: `cerberus-docs/technical/dogfood/2026-08-26-browser-ui-leg-run.md`

**Interfaces:**
- Consumes: everything above; wrangler :8989 running (outside the script, per the env header's prerequisites).

- [ ] **Step 1: Bring the stack up**

```bash
# terminal A: wrangler (pre-existing prerequisite)
cd ../open-agents/apps/api && eval "$(fnm env)" && fnm use 22 && npm run dev
# terminal B: env + run
cd dogfood/realtime-e2e && source ../../scripts/dogfood-realtime-e2e-env.sh && ../../build/cerberus run
```

- [ ] **Step 2: Verify the ui cases in the report**

Expected: `ui-vocab-*` cases present and PASS (missions-conn-status green REQUIRES the #18 fix — that is the point); coverage denominator grew by the assertion count with ui_assert edges credited; `.cerberus/runtime/shots/` empty (no failures) — then temporarily flip one assertion's expected text to a wrong string, re-run, and verify the failure evidence: screenshot file exists + DOM excerpt in the result. Revert the flip.

- [ ] **Step 3: Write the run doc** — verdicts, ui assertion outcomes, the deliberate-failure evidence shape, gaps/next (protocol-coupled assertions, Scout free-leg prompt surface).

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/technical/dogfood/2026-08-26-browser-ui-leg-run.md
git commit -m "docs(dogfood): browser UI leg first live run — ui_assert sweep green, failure evidence shape verified"
```

---

## Self-Review

1. **Spec coverage**: §3.1 atomic action → Task 1+3; §3.2 browser_flow → Task 4; §4 vocab+denominator → Tasks 5+6+8; §5 session/injection → Task 7; §6 evidence → Tasks 1+2+4; §7 env → Task 9; §8 error handling → timeout clamp (T2), auto-screenshot (T4), unreachable (goto failure already fails the case); §9 testing → per-task unit tests + Task 10 live. **Gap**: browser-crash rebuild-once (§8) — folded into Task 2 executor work as a `sync.Once` relaunch guard on Execute error containing "Target closed"; small enough to implement inline during Task 2 without its own task. Scout planning-tool exposure (§10 note) — vocab cases need no prompt surface in v1; deferred with the spec's blessing.
2. **Placeholder scan**: no TBD/TODO; the two "pin during implementation" notes (AuthResult field names, TestCase.Timeout existence) are verification steps against live files, not unspecified work.
3. **Type consistency**: `VocabUIAssertion` field names identical in Tasks 5/6/8; `browser_expect` evidence contract (`Action`/`Matched`/`MatchedType`) identical in Tasks 4 and 8; `NewBrowserExecutor(projectDir, logger)` signature updated at both definition (T2) and caller (T2 step 3).

# Browser UI Test Leg — Design Spec

Date: 2026-08-26
Status: approved-in-principle (brainstorming 2026-08-26; driver decision revised, see §2)

## 1. Motivation

open-agents #18/#19 (web sidebar "Connecting..." forever; refresh failure wiping
auth) were invisible to cerberus: the dogfood observed surface is the API
worker + Durable Object + bridge (HTTP/WS/ACP). The web frontend — the layer
the user actually sees — has zero coverage. #18 is exactly the shape this leg
must catch: **protocol layer true (WS connected), display promise violated
(status text says "Connecting...")**.

Goal: make the rendered DOM a fourth observed surface, with the same
evidence/coverage model as HTTP and WS.

## 2. Decision record

| Decision | Choice | Note |
|---|---|---|
| Assertion primitive | DOM/text assertions + screenshot evidence frames | visual-regression baselines out of scope v1 |
| Driver | **playwright-go (extend existing `BrowserExecutor`)** | REVISED during design: brainstorm initially chose chromedp on the premise "cerberus has no browser dependency" — false. `playwright-go v0.5700.1` is already in go.mod with a working executor (`internal/head/agent/browser.go`). Extending beats rewriting; chromedp would add a second browser stack. |
| Login | HTTP-acquired dev JWT injected into `localStorage['auth-storage']` | real login form out of scope v1 |
| Case generation | UI vocab (deterministic) + Scout free leg | same two-layer model as http vocab sweep + ws_flow |
| Browser lifecycle | one headless Chromium per run; **one page (tab) per case** | single-page executor upgraded; run-level Close (no lingering processes) |

Additional review amendments (A1–A5) folded in: three-syntax selectors
(`text=`/`css=`/`role=` — all native Playwright engines, better than the XPath
approximation originally proposed); `text_absent` polarity = "must not appear
for the whole window, fail fast on appearance"; locale pinned before
assertions (vocab declares expected strings in that locale); tab-per-case
isolation; screenshot labels scoped `{caseID}-{label}.png`.

## 3. Case grammar

### 3.1 New atomic action: `browser_expect`

The one capability the existing four actions lack: a **wait-type assertion**.
`TextContent()` does not wait; async render makes instant checks flaky.

```
Action: browser_expect
Target: <selector>          # text=Connected | css=aside | role=button[name="Run"]
Expectation: <comparator>   # text_present | text_absent | element_visible | element_count>=N
Timeout: seconds (default 10, max 30)
```

- `text_present`: poll until the locator resolves with matching text → pass;
  window expiry → fail.
- `text_absent`: inverse polarity — fail the moment it appears; window expiry
  without appearance → pass.
- `element_count>=N`: poll until locator count ≥ N.
- Implemented via Playwright locators (auto-waiting) + `Locator.WaitFor` with
  `Timeout`; count variant via `Locator.Count()` polling.

### 3.2 New case type: `browser_flow` (Scout free leg)

Mirrors `ws_flow`: a `Steps []TestStep` array executed sequentially in one
page.

```
action: browser_flow
service: open-agents-ui
steps:
  - {action: browser_goto,   url: "/dashboard/missions"}
  - {action: browser_click,  target: "text=独立子任务一"}
  - {action: browser_fill,   target: "css=input[name=prompt]", text: "..."}
  - {action: browser_expect, target: "text=Connected", expectation: text_present, timeout: 15}
  - {action: browser_shot,   label: "after-create"}
```

Step fields reuse the existing `TestStep` struct (Action/Target/URL/Message/
Type/Asserts/Timeout). `url` resolves against the service `base_url`
(`isURL`-relative logic already in `rules_browser.go`).

## 4. UI vocabulary & coverage denominator

Protocol YAML gains a `ui:` section:

```yaml
ui:
  base_url: "http://localhost:5183"
  locale: "en"
  assertions:                 # flat list; id is the denominator unit
    - id: missions-conn-status
      route: "/dashboard/missions"
      target: "text=Connected"
      expectation: text_present
      timeout: 15
    - id: missions-device-counter
      route: "/dashboard/missions"
      target: "text=devices online"
      expectation: text_present
    - id: missions-list-renders
      route: "/dashboard/missions"
      target: "css=aside"
      expectation: element_visible
```

- **Denominator unit = assertion id** (not page). `requiredUIAssertions`
  joins the existing coverage computation; UI coverage is a component of the
  single coverage number, not a separate score.
- `uiVocabCases()` compiles each entry into a deterministic
  `browser_flow` case `[goto, expect]` — same shape as the http vocab sweep.
- `unsupported: true` on an assertion removes it from the denominator with a
  stated reason (same escape hatch as WS edges).
- v1 covers **static promises only** (assertions true of the page itself).
  Protocol-coupled assertions ("mission card shows 3 tasks 100%" — value
  sourced from protocol-side evidence via `{{...}}` templates) are a follow-up.

## 5. Session & auth injection

Run-level, once per run, before the first UI case:

1. HTTP `POST /api/dev/setup` + `POST /api/dev/login` (existing dev-user
   flow) → JWT pair.
2. New page → `goto base_url` (bare visit establishes the origin) → JS
   evaluate: write the zustand persist blob
   `localStorage['auth-storage'] = {state:{user, token, refreshToken, ...}, version:0}`
   plus the locale key (i18n pin, amendment A3).
3. Subsequent cases open fresh pages; localStorage persists per browser
   context (one context per run), so injection happens once.

Per-case isolation (A4): every `browser_flow` (and every vocab case) opens a
new page and closes it at case end. The legacy single shared page stays for
atomic `browser_*` actions (back-compat).

## 6. Evidence

`types.BrowserResult` extended with assertion fields (`Selector`,
`Expectation`, `Observed`, `AssertOK`). Frame types stay inside the existing
result plumbing:

- every step → `BrowserResult` (action + observed text/URL/title/latency)
- `browser_expect` → result carries expected vs observed + pass/fail
- `browser_shot` (and auto-capture on any step failure) → PNG written to
  `.cerberus/runtime/shots/{caseID}-{label}.png`; evidence frame stores the
  path (not base64) plus a 2 KB `document.body.innerText` excerpt

Assertion evaluation is executor-side and deterministic; the Examiner judges
"why did it fail" (SUT display defect vs bad assertion), not whether it
failed.

## 7. Environment orchestration (dogfood realtime-e2e)

- Web UI served via `vite build && vite preview --port 5183` in the env
  script — **not** `vite dev`: preview has no file watchers (avoids the
  inotify ENOSPC failure that killed dev mid-demo) and is deterministic.
  One-time build cost paid per env start.
- wrangler (:8989) and bridges unchanged; the UI leg requires both (the UI
  talks to them), so vocab assertions on connection status implicitly verify
  the whole stack.
- Bridge/actor config unchanged; the `open-agents-ui` service entry
  (base_url) lives in the protocol YAML `ui:` section.

## 8. Error handling

| Failure | Handling |
|---|---|
| `browser_expect` timeout | case fail; evidence = DOM text excerpt + auto screenshot (the #18 catch shape) |
| goto fails / 5xx | case fail, `ui_target_unreachable` → existing `target_unreachable` escalation checkpoint |
| browser process crash | rebuild BrowserSession once; second crash disables the UI leg only (other legs unaffected) |
| step timeout ceiling | 30 s hard cap per step |
| run end | browser + context closed (no lingering Chromium — matches the leave-no-process discipline) |

Repair eligibility: `browser_flow` joins the non-repairable set alongside
`browser` atomic actions (repair emits HTTP/WS shapes only).

## 9. Testing strategy

- Unit: assertion polarity (present/absent/count) against a scripted fake
  page; selector-syntax dispatch; vocab compilation (yaml → cases); locale
  injection blob shape.
- Live: dogfood realtime-e2e run with the `ui:` vocab present — expects all
  UI assertions green post-#18-fix; one deliberately-failing assertion
  (temporarily wrong expected text) verifies the failure evidence shape
  (screenshot + DOM excerpt) end to end.

## 10. Out of scope (v1)

Visual-regression baselines (screenshots are evidence only), protocol-coupled
assertions, login-form coverage, Firefox/WebKit, replacing the atomic
`browser_*` actions, Scout prompt changes beyond exposing `browser_flow` in
the planning tool surface.

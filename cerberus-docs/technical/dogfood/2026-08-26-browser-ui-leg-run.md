# Browser UI Leg — First Live Run (Run 25)

Date: 2026-08-26
Branch: `feat/browser-ui-leg` (cerberus); open-agents main `1cbe428`
Spec: `cerberus-docs/superpowers/specs/2026-08-26-browser-ui-leg-design.md`

## What shipped

cerberus's fourth observed surface — the rendered DOM:

- `browser_expect` wait-type assertion (polarity A2: `text_absent` fails fast
  on appearance; 30s hard window cap) + `browser_shot`/failure screenshots
  to `.cerberus/runtime/shots/{caseID}-{label}.png`
- `browser_flow` case type in the deterministic Steps runner (goto/click/
  fill/expect/shot verbs), excluded from repair
- UI vocabulary (`ui:` section): 4 assertions in the dogfood vocab = 4 new
  coverage-denominator units (`ui_assert` edges, `browser`→`web_ui`),
  credited by matched `browser_expect` evidence
- Run-level session injection: web-actor email/password → POST /api/auth/login
  → zustand `auth-storage` blob (token + refreshToken — the WS gate needs
  both) + `i18nextLng` locale pin, written once per run
- Env: `vite build && vite preview :5183` (NOT dev — dev's watchers hit
  inotify ENOSPC)

Driver note: brainstorm initially chose chromedp on the false premise that
cerberus had no browser dependency; the existing playwright-go
`BrowserExecutor` was extended instead (recorded in the spec §2).

## Run 25 (chromium missing at run time)

705 pass / 9 fail, coverage 98.98% (5 gaps), 34m24s, 714 verdicts.

Fails: the 4 `ui-vocab-*` cases + `ws-realtime-wf-mission-fanout` (GLM
5-hour rate limit hit during the run — the examiner reflection failure in
the log is the same cause). The ui-vocab cases failed because the
playwright-go driver wanted chromium build **1200** while the host had
1208/1217/1223 — the browser plugin never registered ("Executable doesn't
exist ... chromium_headless_shell-1200"). The failures themselves prove the
plumbing end-to-end: vocab assertions were generated, executed as real
cases, reported as fail, and counted as coverage gaps.

Fix: `go run github.com/playwright-community/playwright-go/cmd/playwright
install chromium` (build 1200 installed).

## Post-fix live validation (full chain, in-run equivalent)

With the right chromium, the exact vocab flow was driven live
(wrangler :8989 + vite preview :5183):

| assertion | verdict | observed |
|---|---|---|
| missions-conn-status (`text=Connected`) | PASS | `"Connected"` |
| missions-device-counter (`text=devices online`) | PASS | `"wifi3 devices online"` |
| missions-list-renders (`css=aside`) | PASS | visible |

missions-conn-status is the #18 regression guard: on pre-660d41f code this
assertion times out with observed `""` — exactly the defect class this leg
exists to catch (protocol true, display lying).

Deliberate-failure evidence shape verified: wrong expected text →
`expect text_present "text=Disconnected": not satisfied within window
(observed "")` + 127KB screenshot at `.cerberus/runtime/shots/`.

Preserved as `//go:build integration` test:
`internal/head/agent/browser_session_integration_test.go`.

## Follow-ups

- Run 26 (next GLM window) should show the 4 ui-vocab cases green and the
  ui_assert edges credited — coverage back to 100%.
- Scout free-leg prompt surface for browser_flow (spec §10 deferral).
- Protocol-coupled assertions (mission-card task counts via `{{...}}`).
- Hydration latency: first goto after a cold browser can sit in
  "Loading..." for several seconds — the wait-type assertion absorbs it,
  but vocab timeouts below 10s would flake.

# Run 26 — Browser UI Leg Green (3/4), Full-Chain Validated

Date: 2026-08-26
cerberus main (post 35f14a5); stack: wrangler :8989 + vite preview :5183

## Result

**715 pass / 3 fail / 1 recovered, coverage 99.74% (2 gaps), 39m54s, 845K
tokens, 719 verdicts, kiro zero-launch.** Browser plugin registered
(chromium build 1200 installed post-run-25).

UI vocabulary outcomes:

| assertion | verdict | note |
|---|---|---|
| missions-conn-status (`text=Connected`) | PASS | **the #18 regression guard is live** — pre-660d41f code fails this |
| missions-device-counter (`text=devices online`) | PASS | observed with 3 demo bridges online |
| missions-list-renders (`css=aside`) | PASS | |
| devices-page-populated (`css=table tbody tr`) | FAIL | **wrong assertion, not a SUT defect**: the devices page renders a card grid; live probe: `css=table` matches 0, `text=dev-device` matches 753 cards |

Fixed post-run: the assertion now targets `text=dev-device`
(element_count>=1) with the wrong-selector lesson recorded in the vocab
comment. This is the leg working as designed — the failure evidence
(observed 0 vs expected >=1) let us distinguish "display lying" from "we
asserted the wrong place" without touching the SUT.

The other 2 fails are pre-existing families, not UI-leg related:
`ws-realtime-bridgeacp-acpreal-session` (1 auth — transient gateway flap on
the real-LLM leg) and `ws-realtime-wf-mission-fanout` (recurring ws_match
family + a repair attempt). 1 recovered elsewhere.

## Coverage note

2 gaps at 99.74%: the devices-page assertion gap (fixed) plus one WS-edge
gap from the fanout fail. Next full run should return to 100% with the
devices assertion green.

## Follow-ups

- Scout free-leg prompt surface for browser_flow (spec §10 deferral).
- Protocol-coupled assertions (mission-card task counts via `{{...}}`).
- Hydration latency absorbed by wait-type assertions; keep vocab timeouts
  >= 10s.

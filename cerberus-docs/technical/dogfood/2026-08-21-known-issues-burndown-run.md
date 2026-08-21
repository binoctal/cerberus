# Known-issues burndown — verification runs (2026-08-21)

Three full dogfood runs today against the open-agents
`fix/known-issues-burndown` branch (merged to main as fa06516; bridge
submodule 279d2f8) and two cerberus filter fixes (merged as d8755d6).

## Fixes shipped (open-agents)

| # | Fix | Live evidence |
|---|---|---|
| 2 | dead worker.ts dev/setup dup removed | 1757 API tests green |
| 3 | worker.ts mounts routes/auth/index (sessions+tokens live) | `/api/auth/sessions` → 401 not 404 |
| 7 | dispatch path emits `workflow:task_started` | unit test + wiring before the observed progress-0 frame |
| 10 | WorktreeManager absolutizes base | bridge log: session workDir = absolute worktree path |
| 11 | plans PUT/DELETE invalidate plan-limits cache | behavior test (mock-D1 artifact documented in test) |
| 1 (partial) | `session:cancelled` added to DO bridge→web whitelist | no awaiting case yet — vocab follow-up |

## Cerberus follow-on fixes (found by these runs)

- **tc-004 family** (`process_bound` role flag) — run 1 verified: zero
  bridge-role connects in the plan.
- **WS template sub-path drift** — run 2's tc-001 (`GET /ws/user-1/health`
  → 426) exposed that `filterWSEndpointDrift` matched declared WS paths
  exactly; `wsPathMatcher` now treats `{param}` segments as wildcards and
  matches the whole template subtree. Run 3 verified: only `tc-002` in the
  plan, sub-path cases dropped.

## Run results

| Run | Session | Verdicts | Notes |
|---|---|---|---|
| 1 | 4f91e5c9 | 691 pass / 0 fail / 2 uncertain | cerberus-only fixes, open-agents main |
| 2 | f3b1e220 | 690 pass / 1 fail / 1 skip / 1 uncertain | tc-001 sub-path drift (cerberus gap); task-assign uncertain |
| 3 | e23402b5 | 691 pass / 1 fail / 0 uncertain | filters verified; task-assign judge-downgraded (executor pass) |

## Open item: `ws-realtime-wf-task-assign` judge flake

Across runs 1-3: pass(0.75) → uncertain(L3) → fail(0.6) with the same
binaries; run 3's executor status was pass and the judge downgraded on
evidence strictness (membership facts show `recipients=[]` for the three
sends). The 5s post-marker window case is timing-sensitive. NOT a
regression from today's fixes (mission-seed chain, including the
absolute-worktree PTY session + callbacks, passed in every run). Next
step if it recurs: widen the case's evidence window or teach the judge
the shim-exit semantics.

## Remaining known issues (not addressed)

- #1 (rest): DO whitelist vs bridge handlers — `session:resume`,
  `scanner:rules:sync`, merge/cleanup workflow commands; `device:listDir`
  has no bridge handler. Needs an alignment decision (add handlers vs
  drop whitelist entries).
- #4: workflow/multiagent naming fork — documentation constraint.
- #5: `workflow:job_completed` / `task_status_update` have no emitter —
  needs an emit-or-drop decision.
- #9: `[QUESTION]` PTY echo false positive — needs echo-detection design.
- #12: plan-limits deep-merge fallback — by design; dogfood seeds
  explicit limits.

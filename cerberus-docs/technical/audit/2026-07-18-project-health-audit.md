# Project Health Audit — 2026-07-18

Scope: `internal/` + `cmd/`. Method: `go test -cover` per package, `go tool cover -func` for per-function, structural grep for bug/concurrency patterns. 29 packages, ~31k non-test LOC.

## Headline

The codebase is in good shape: no oversized files (max 450 LOC), only 2 `panic()` calls (both legitimate — registry lookup at init, constructor arg validation), tests green, and coverage is genuinely high after correcting one tooling artifact. The single most actionable finding is a **coverage-reporting artifact**, not a code defect.

## P0 — `go test -cover` misreports `internal/ai` as 0.0%

`go test -cover ./internal/ai/` prints `coverage: 0.0%`, yet the generated profile is valid and `go tool cover -func` reports **86.8%**. The package has 99 passing tests across 18 test files; this is a Go 1.26 display/aggregation artifact (path mismatch between the cover summary and the profile), not missing tests.

**Why it matters:** any script, Makefile target, or dashboard that reads the `coverage: X%` line from `go test` will record 0% for the largest LLM-interaction package and silently miscalculate project-wide coverage. `make check` does not currently gate on a coverage threshold, so this is latent.

**Action:** audit `make test`/coverage tooling to derive percentages from `go tool cover -func` (or `go test -coverprofile` + `go tool cover -func=profile`) rather than the inline `go test -cover` line. Confirm whether `internal/ai` is the only affected package by diffing the two methods across all packages.

## P1 — Real coverage gaps

Genuine low-coverage packages (per `go tool cover -func`):

| Package | Coverage | Gap character |
|---|---|---|
| `internal/autotest` | 51.3% | Node/Python/Mocha coverage providers are entirely untested (need real runtimes); `RunCoverage`/`DefaultGoCoverageRunner` for Go also untested (only `parseCoverProfile` is covered, added with examiner real-coverage) |
| `internal/store` | 63.7% | Review untested CRUD/error paths |
| `internal/validation` | 65.5% | Review |
| `internal/sandbox` | 66.7% | Review |

The autotest gap is the most structural: the language providers are the integration boundary with Node/Python toolchains and currently have zero execution coverage. `parseCoverProfile` for Go is covered, but the runner that invokes `go test -coverprofile` is not.

## P2 — Goroutine lifecycle (review, not confirmed leak)

- `internal/mcp/tool_handlers_run.go:71,77` — guarded by `runCtx, cancel := WithCancel(ctx)` + `defer cancel()`. **Healthy.**
- `internal/head/agent/process_mgr.go:104` — background goroutine; no visible `ctx.Done()` in the file. Likely driven by process exit, but the lifecycle should be confirmed (what cancels it if the process hangs?).
- `internal/head/agent/mcp_exec_stdio.go:62` — same pattern; confirm the pump goroutine exits on reader EOF / ctx cancellation.

Action: read both and confirm every `go func` has a documented exit condition. Low probability of a real leak given `make test -race` is green, but worth a 15-minute lifecycle pass.

## P3 — Tech debt (low)

- Largest file `cmd/cerberus/memory_command.go` (450 LOC) — acceptable for a CLI subcommand, but a candidate to split if it grows.
- Only one genuine `TODO` existed in non-test code (`react_loop_helpers.go` per-service actor) — **fixed today** in `4e1cc7b`. The remaining `TODO`/`FIXME` grep hits are all legitimate patterns consumed by the comment-miner (`internal/ai/comment_miner_*`), not work markers.
- ReAct path now aligns with the rule path on all three per-service dimensions (service headers, base URL, actor headers) — verified by structural scan; no remaining `actors[0]`/`services[0]` hardcoding outside the rule engine's documented fallback.

## Not a finding (ruled out)

- `internal/ai` "0% coverage" — see P0; artifact only.
- Widespread tech debt — not present at current scale.
- Missing tests on core heads (agent 79.8%, session 80.6%, examiner 86%, scout 89%) — all adequate.

## Suggested next work order

1. **P0** — fix coverage reporting derivation (small, high signal; unblocks trustworthy coverage gating).
2. **P2** — goroutine lifecycle pass on `process_mgr.go` / `mcp_exec_stdio.go` (quick, removes a latent risk class).
3. **P1 autotest** — add Go `RunCoverage` runner test (mockable command runner); defer Node/Python provider tests until a fixture runtime decision is made.
4. **P1 store/validation/sandbox** — targeted coverage of error/edge paths only where they protect data integrity.

# Runs 19-21 — ACP coverage package: two gateway-flaky runs, one placeholder root cause, first real-LLM pass

Date: 2026-08-25
Sessions: 19 = 7d99df90 (702/6, judge down), 20 = 0434cba7-era (704/4,
211K), 21 = final (707/2, judge healthy 745K).
Implements: `cerberus-docs/superpowers/specs/2026-08-25-acp-coverage-design.md`

## What shipped (branch feat/acp-coverage)

- `ProtocolRole.ACPCli` / `ACPReal` — SUT fact in YAML; `acpE2ECases`
  emits a deterministic ACP-path case per acp_cli role (fake agent) and a
  real-LLM case per acp_real role (layer separation keyed on the flag:
  acp_real devices have no shim on PATH, so they get ONLY the real leg —
  and reale2e skips them).
- `shim/npx` routes `@agentclientprotocol/claude-agent-acp` to
  `shim/acp-fake-agent` (initialize — MUST carry agentInfo or the adapter's
  initDone never closes — / session/new / session/prompt / session/close;
  any other request gets an empty result).
- Fourth real actor `bridge-acp-real`: real npx → real claude-agent-acp →
  real claude → GLM gateway LLM, env inherited.

## Run 19-20: not evidence about ACP

GLM gateway outage/flakiness window: judge degraded on every verdict
("Judge failed, using execution status"; run 19 spent ~10K tokens), the
mission planner failed (mission-seed/fanout fail), and the real-LLM leg
timed out — all LLM paths share the gateway. The fake layer passed even
during the outage (deterministic). Also surfaced: two generator layer-
pollution bugs (fixed, 17473ba) and the log-loss red herring (restart
instances append, but initial instances of bridge-acp-real never logged —
see below).

## Run 21 root cause: placeholder charset

`ws-realtime-bridgeacp-acpreal-session` failed twice with a silent empty
send. Wrangler log: `sendToBridge] Device not found: {{bridgeacp-role}}`
— the placeholder went out UNRESOLVED: `wsBodyPlaceholderRe` =
`[A-Za-z0-9_.:]` has NO hyphen, so `{{bridge-acp.deviceId}}` never matched
and passed through verbatim. Manual decisive experiment first: same
device identity, same D1, same wrangler, shell-launched bridge → full
real-ACP/real-LLM success ("Claude Agent SDK ready to help") — proving
the path itself was fine and pinning the fault on the harness side.

Fix (c3069e2): '-' joins the charset; a dot placeholder naming an
UNDECLARED role is now a hard error (was: literal passthrough — the same
silent-send bug class). Dogfood role renamed bridge-acp → bridgeacp.

Run 21 outcome: **acpreal PASS (attempts:1, judge 0.82) — the dogfood's
first real-CLI real-LLM execution**; all three fake-layer acpe2e PASS.

## Run 21 residuals (fixes in flight for run 22)

- mission-seed failed: the fail mission's claude(ACP) fallback rung hit
  the fake agent, which answered SUCCESS → task_failed never fired →
  mission-seed fail leg timed out and its four edges became the coverage
  gaps (task_failed/task_error/task_question/+1; 99.2%). Fix: fake agent
  errors+exits on CERBERUS_FAIL prompts (PTY-shim parity).
- fanout failed on the third task_completed receive (job_status completed
  arrived — finalize implies all done). Fix: 2× task_completed +
  job_status finalizer as the completion assertion.
- Cerberus repair-loop bug recorded: the repair planner (LLM) emitted a
  replacement case with an EMPTY target ("target: unknown" in the
  verdict) → resolveProtocol nil → 'ws connect: unknown role "web"' —
  repair cases should inherit/validate the original case's Target.
- Bridge initial-instance logs absent for bridge-acp-real (only restart
  instances logged) — unexplained, non-blocking, worth one look later.

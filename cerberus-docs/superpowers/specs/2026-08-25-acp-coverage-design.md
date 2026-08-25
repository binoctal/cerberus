# ACP Protocol Coverage — Design Spec

Date: 2026-08-25
Status: approved in session (dual-layer: deterministic shim + real leg; real
leg over ACP; Web UI demo follows as the next deliverable)
Closes: the dogfood's "real AI CLI executes a task" gap AND the ACP-adapter
coverage gap (today every dogfood session lands on PTY; the ACP path is
only exercised by one flaky bridge-internal test).

## Goal

Exercise the bridge's REAL ACP adapter (initialize → session/new →
session/prompt → session/update → session/close) end-to-end in the dogfood
suite, twice:

1. **Deterministic layer** (every run, zero LLM): a fake `npx` in the shim
   dir answers ACP JSON-RPC for `@agentclientprotocol/claude-agent-acp`
   invocations. The three replica bridges' PATH has the shim dir first, so
   `session:start` with `cliType: "claude"` (→ `npx …claude-agent-acp`)
   connects to the fake agent and completes deterministically.
2. **Real layer** (one real LLM call per run): a FOURTH bridge actor
   `bridge-acp-real` whose PATH does NOT include the shim dir — its `npx`
   is the real one, which spawns real claude with the inherited GLM gateway
   env (ANTHROPIC_BASE_URL/AUTH_TOKEN). The same session:start shape routes
   a real prompt through the real adapter to a real LLM reply.

## Components

### 1. Fake ACP agent + npx router (dogfood shim dir)

- `shim/acp-fake-agent` (python3, stdlib only): line-delimited JSON-RPC on
  stdin/stdout. Implements exactly what the bridge adapter needs:
  - `initialize` → result `{protocolVersion:1, agentInfo, capabilities:{}}`
  - `session/new` → result `{sessionId:"cerberus-acp-<n>"}`
  - `session/prompt` → one `session/update` notification
    `{params:{sessionId, update:{sessionUpdate:"agent_message_chunk",
    content:{type:"text", text:"CERBERUS_ACP_OK"}}}}` then the prompt
    response `{result:{stopReason:"end_turn"}}`
  - `session/close` → empty result, then exit 0
  - any other request → empty result (forward-compat)
  - stderr silent (the adapter suppresses npm noise anyway)
- `shim/npx` (sh): if `$1` != `@agentclientprotocol/claude-agent-acp`,
  exec the first REAL npx on PATH outside the shim dir (pass-through —
  wrangler/tooling must keep working); else exec the fake agent.

### 2. Fourth actor: bridge-acp-real

- Not a replica (its env differs): `fidelity: real-process`, pairs via the
  same dev backdoor (4th device under the dev user), HOME isolated,
  capture deviceId, ready pattern identical — but env.PATH WITHOUT the
  shim dir (real npx + real claude visible).
- Protocol role `bridge-acp` (credential_ref bridge-acp-real,
  process_bound, acp_cli: claude, acp_real: true).

### 3. Scout generator: acpE2ECases

- New `ProtocolRole.ACPCli` (`yaml:"acp_cli,omitempty"`) + `ACPReal`
  (`yaml:"acp_real,omitempty"`): SUT fact "this real role's device runs
  this CLI over the ACP adapter" lives in YAML.
- `acpE2ECases(svc, realRoles)` emits, per role with ACPCli:
  - ONE deterministic case (ID `<svc>-<role>-acpe2e-session`): connect as
    web, `session:start {cliType: <acp_cli>, workDir:/tmp, deviceId:
    {{role.deviceId}}}`, expect `session:started`, `session:send` content
    "Reply with exactly: CERBERUS_ACP_OK", expect `chat:response`
    (aliases `session:output-batch`), `session:stop`, `stopped`. Judge
    checks the CERBERUS_ACP_OK content. Timeout 30s per receive.
  - If ACPReal: ONE real case (ID `<role>-acpreal-session`), same shape,
    prompt "Reply with one short sentence confirming you are a real AI
    agent. Do not create files.", receives at 120s (real LLM latency),
    expectation tolerant of content ("a substantive human-readable reply,
    not an error").
- Claims: role claims via roleClaimBindings (unchanged union).

### 4. Dogfood config

- project.yaml: add bridge-acp-real actor (4th real process; ~zero LLM
  cost when idle).
- protocols: `bridge-acp` role block.
- No ledger change (schedule-real-cli stays wont-test — the L2 integration
  suite remains its prover; the ACP cases are evidence-rich additions, and
  the real leg's claim story can be revisited later).

## Error handling / risks

- Real claude via GLM gateway: same env this cerberus session uses; if the
  gateway flakes, the real case fails and the repair loop retries —
  acceptable noise (one case), does not gate.
- The fake agent must not hang on unknown input: every request gets a
  response; EOF on stdin exits.
- `session/prompt` while processing: the fake answers immediately, no race.

## Testing ladder

1. Shim: drive `shim/acp-fake-agent` with a scripted JSON-RPC conversation
   (assert the exact four responses). Unit-test the scout generator
   (acp_cli gating, case shapes, real-role emission).
2. `make check`; bridge/apps/api untouched this round.
3. Live run 19: acpe2e cases ×3 (one per replica bridge) pass
   deterministically; acpreal case passes with a real LLM reply; suite
   stays green; coverage 100%; ~1 extra LLM call in budget.

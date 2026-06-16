# Cerberus MCP Server — Design Spec

**Date**: 2026-06-11
**Status**: Approved

## Goal

Enable developers to use Cerberus from within Claude Code CLI — trigger tests, get results, and intervene only on critical events — without leaving the conversation.

## Design Principles

1. **Autonomous by default** — Cerberus runs fully autonomously (Scout ToT → Agent ReAct → Examiner Judge/Critic → strategy learning)
2. **Escalate only on critical events** — User intervention is the exception, not the rule
3. **Zero extra configuration** — Shares the same `ANTHROPIC_API_KEY` as Claude Code
4. **Per-project isolation** — Each project has its own `.cerberus/` config + SQLite DB
5. **Pure incremental** — No changes to existing Go code behavior, only additions

## §1 Escalation Mechanism

Cerberus runs fully autonomously. User intervention is triggered **only** when the AI cannot safely decide on its own.

### Critical Events (4 types only)

| Event | Trigger | Why user must decide |
|-------|---------|---------------------|
| **Budget warning** | Token usage ≥ 80% with > 5 remaining test cases | Involves cost — AI cannot spend user's money |
| **Systemic failure** | Consecutive test failures ≥ threshold (likely environment down) | Continuing is waste, but user knows if env is expected to be flaky |
| **Destructive risk** | Action is DELETE/DROP or modifies production data | Data safety — AI cannot take irreversible actions |
| **Target unreachable** | Connection timeout ≥ N consecutive attempts | User knows whether to wait, switch URL, or abort |

### All other decisions are autonomous

- Scout can't pick best plan → pick highest confidence plan
- Agent recovery fails → mark case as skipped, continue
- Examiner uncertain → mark as uncertain with low confidence, continue
- Strategy matching misses → no match, proceed without strategy

### MCP Tools

| Tool | Description |
|------|-------------|
| `cerberus_run` | Start test session with goal + url, returns session_id immediately |
| `cerberus_status` | Poll progress (phase, completed/total, issues found) |
| `cerberus_report` | Get final report (pass/fail/skip + evidence + strategy summary) |
| `cerberus_decide` | Provide user decision on critical event |
| `cerberus_cancel` | Cancel running session |

### Checkpoint Injection Points

Escalation checkpoints are injected in `internal/head/agent/executor.go`, not in `session/lifecycle.go`:

| Checkpoint | Injection point in `executor.go` |
|------------|----------------------------------|
| Budget warning | After `r.steer()` returns, before next iteration of ReAct loop |
| Systemic failure | After each `executeStep()` in `ExecutePlan()`, track consecutive failures |
| Destructive risk | Before `r.executor.Execute()`, inspect `action.Type` and `action.Method` |
| Target unreachable | After `r.executor.Execute()` returns timeout/error, track consecutive timeouts |

`session/lifecycle.go` passes `EscalationGate` through to Agent head at construction time. No changes to lifecycle.go itself.

### Interaction Flow

```
Cerberus runs session.Run() in goroutine (autonomous)
  ↓
4 escalation checkpoints in agent/executor.go (via EscalationGate interface)
  ↓
Checkpoint triggered → event sent to escalation channel → goroutine blocks
  ↓
cerberus_status returns {status: "pending_decision", event: {...}}
  ↓
Claude Code prompts user → user decides
  ↓
cerberus_decide sends decision to channel → goroutine resumes
```

## §2 MCP Server Architecture

### New subcommand

```bash
cerberus mcp    # Start MCP server (stdin/stdout JSON-RPC)
```

Claude Code launches this via project-level `.claude/settings.json`:

```json
{
  "mcpServers": {
    "cerberus": {
      "command": "cerberus",
      "args": ["mcp"]
    }
  }
}
```

### LLM API Key

Cerberus reads `ANTHROPIC_API_KEY` from the same environment as Claude Code. No separate configuration.

### Data Flow

```
Claude Code                    Cerberus MCP Server
    │                                  │
    ├─ cerberus_run(goal, url) ───────→│ Scout → Agent → Examiner (autonomous)
    │←── session_id ──────────────────┤
    │                                  │
    ├─ cerberus_status() ────────────→│ returns progress
    │←── {phase, completed, total} ───┤
    │                                  │
    │        ... polling ...            │   escalation event fires
    │                                  │
    ├─ cerberus_status() ────────────→│
    │←── {status: "pending_decision", ┤── budget 82% used, 18 cases left
    │     event: "budget_warning"}     │
    │                                  │
    │  "继续跑"                        │
    ├─ cerberus_decide("continue") ──→│ resumes execution
    │                                  │
    ├─ cerberus_status() ────────────→│
    │←── {status: "completed"} ───────┤
    │                                  │
    ├─ cerberus_report() ────────────→│
    │←── full report ─────────────────┤
```

### Concurrency Model

- `session.Run()` runs in a goroutine
- Escalation channel: buffered chan, goroutine sends event then blocks on receive
- `cerberus_decide` writes to channel → goroutine unblocks
- `cerberus_cancel` cancels context → goroutine exits

### Polling Guidance

Claude Code doesn't auto-poll. The `cerberus_run` tool description includes instruction: *"After calling this tool, periodically call cerberus_status to check progress. Stop when status is 'completed' or 'failed'."* This ensures Claude Code knows to poll without requiring a separate Skill wrapper.

## §3 Per-Project Setup

### `cerberus init` Enhancement

Running `cerberus init` in a project directory now also:

1. Creates `.cerberus/project.yaml` + `credentials.yaml` (existing)
2. Seeds default strategies into `.cerberus/cerberus.db` (existing)
3. **New**: Writes MCP server config to `.claude/settings.json`

If `.claude/settings.json` already exists: read → merge `mcpServers.cerberus` → write back. Never overwrite other settings. The operation is idempotent — handles 4 cases:

1. File doesn't exist → create with `mcpServers.cerberus`
2. File exists, no `mcpServers` key → add `mcpServers.cerberus`
3. Has `mcpServers` but no `cerberus` → add `cerberus` entry
4. Already has `cerberus` → skip (no-op)

### Usage in Another Project

```bash
cd /path/to/modelsite
cerberus init           # Creates .cerberus/ + configures .claude/settings.json
vim .cerberus/project.yaml  # Edit project details
# Open Claude Code in this directory → Cerberus MCP auto-available
```

User in Claude Code:

```
用户: "测试 modelsite 的支付 API"

Claude Code → cerberus_run(goal="测试支付API", url="http://localhost:3000")
           → reads .cerberus/project.yaml for project config
           → Scout → Agent → Examiner (autonomous)
           → returns report

Claude Code: "支付 API 测试完成：
             ✅ POST /api/orders — 通过
             ❌ POST /api/payments/webhook — 失败（500）
             ⚠️ POST /api/refunds — 不确定"
```

### Multi-Project Isolation

- Each project has its own `.cerberus/` directory (config + SQLite)
- MCP server reads config relative to working directory
- Strategy learning is per-project
- No cross-project data leakage

### Crash Recovery

If `cerberus mcp` crashes mid-run, the SQLite session remains in `running` status. On next `cerberus mcp` startup, a cleanup pass marks all `running` sessions as `interrupted`. Users can re-run the same goal. Strategy learning from completed test cases is preserved.

## §4 Implementation Scope

### New Code

| Module | File | Responsibility | Lines (est.) |
|--------|------|----------------|-------------|
| `internal/mcp/` | `server.go` | MCP protocol (stdin/stdout JSON-RPC) | ~150 |
| `internal/mcp/` | `tools.go` | 5 tool definitions + handlers | ~150 |
| `internal/mcp/` | `escalation.go` | Checkpoint detection + pause/resume via channel | ~100 |
| `cmd/cerberus/` | `main.go` | New `mcpCmd()`, enhanced `initCmd()` | ~50 |

### Modified Code

| File | Change | Lines (est.) |
|------|--------|-------------|
| `session/lifecycle.go` | Pass `EscalationGate` to Agent head at construction | ~10 |
| `internal/head/agent/executor.go` | Insert 4 escalation checkpoints in ReAct loop + ExecutePlan | ~50 |

**Total: ~520 lines new/modified**

### Escalation Checkpoint Interface

```go
// EscalationGate is called at critical points during session execution.
// Returns user decision or empty string (continue autonomously).
type EscalationGate interface {
    Check(ctx context.Context, event EscalationEvent) EscalationDecision
}

type EscalationEvent struct {
    Type       string  // "budget_warning" | "systemic_failure" | "destructive_risk" | "target_unreachable"
    Message    string
    SessionID  string
    Data       map[string]any
}

type EscalationDecision struct {
    Action  string  // "continue" | "abort" | "skip_case"
    Payload string  // Optional: e.g. modified URL for "target_unreachable"
}
```

In production (MCP mode): `MCPEscalationGate` sends event to channel, blocks until `EscalationDecision` arrives.
In CLI mode: `NoOpEscalationGate` always returns `{Action: "continue"}` (autonomous, no MCP).

This ensures existing tests and CLI behavior are unaffected.

### Non-Goals (for this phase)

- Auto-detect project type / tech stack from code
- Multi-model LLM support through MCP (Cerberus handles its own LLM)
- Real-time streaming of test execution (polling is sufficient)
- Web UI for test results

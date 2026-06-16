# LLM Provider Detection by CLI Identity — Design Spec

**Date**: 2026-06-13
**Status**: Approved

## Goal

Determine the LLM provider from **which CLI hosts cerberus** (Claude Code today;
Codex / Gemini CLI later) instead of guessing it from the model name. When
cerberus runs under Claude Code, the provider is `anthropic` by fact, not by
coincidence of an unknown model prefix.

## Context / Problem

Provider awareness is currently scattered across three call sites and **all of
them derive it from the model name**:

- `internal/config/config.go` `resolveAPIKey` — switches on model prefix
  (`gpt` → `OPENAI_API_KEY`, `gemini` → `GEMINI_API_KEY`, else `ANTHROPIC_*`).
- `internal/config/config.go` `resolveBaseURL` — hardcoded to read only
  `ANTHROPIC_BASE_URL`.
- `internal/config/config.go` `resolveModel` — reads only
  `ANTHROPIC_DEFAULT_SONNET_MODEL`.
- `internal/llm/client.go` `detectProvider` — model prefix again.

Consequence: `glm-5.1` resolves to `anthropic` only because *unknown prefixes
default to anthropic*. That is a coincidence, not a guarantee. The real reason
cerberus works under Claude Code is that **it is Claude Code** — which always
speaks the Anthropic protocol. The CLI identity is the actual source of truth,
and today nothing reads it.

### Evidence this is feasible

Verified live in a Claude Code session: Claude Code injects its own identity
markers into every subprocess env, independent of any user config:

- `CLAUDECODE` — set whenever running under Claude Code (Bash tool, `!`
  command, `cerberus mcp`, subagents).
- A family of `CLAUDE_CODE_*` (`ENTRYPOINT`, `EXECPATH`, `SESSION_ID`,
  `CHILD_SESSION`, `SSE_PORT`) and `TERM_PROGRAM`.

The process tree also contains a `claude` ancestor, but process-tree walking
needs per-platform `/proc` handling and distorts under containers / nested
shells — so it is deliberately excluded (see Out of Scope).

## Design Principles

1. **CLI identity is the source of truth** — detection answers "which CLI am I
   under", which deterministically implies provider + config file + env prefix.
2. **Direct mapping, no model-name guess** — Claude Code → `anthropic` always.
3. **YAGNI** — ship only the Claude Code mapping (which we can verify), behind
   an extensible interface.
4. **Pure incremental** — detection adds a layer above existing `resolve*`;
   behavior for the unsupported case (`CLIUnknown`) is unchanged from today.

## §1 New package `internal/detect/`

```go
type CLI string
const (
    CLIClaudeCode CLI = "claude-code"
    CLIUnknown    CLI = "unknown"
)

// Profile bundles everything a detected CLI implies.
type Profile struct {
    CLI           CLI
    Provider      string // "anthropic" | "openai" | "gemini"
    SettingsFile  string // config file to read, e.g. ".claude/settings.json"
    EnvPrefix     string // "ANTHROPIC" | "OPENAI" | "GEMINI"
}

type Detector interface {
    Detect() (Profile, bool)
}
```

`ClaudeCodeDetector`: `os.Getenv("CLAUDECODE") != ""` → returns
`Profile{CLIClaudeCode, "anthropic", ".claude/settings.json", "ANTHROPIC"}`.
Detection touches **only `CLAUDECODE`** — no process-tree, no multi-signal
voting. Single env var, single reason.

`Detect()` runs a registry of detectors in order; the first hit wins. All miss
→ `Profile{CLI: CLIUnknown, ...}` (empty fields), and existing behavior takes
over unchanged.

## §2 `config.Load()` integration

`Load()` runs `detect.Detect()` once at the top and stores the resulting
`Profile` on `Config`. The three resolvers become **prefix-parameterized**
rather than hardcoded to `ANTHROPIC_*`:

- `resolveBaseURL` reads `${EnvPrefix}_BASE_URL` (today: `ANTHROPIC_BASE_URL`).
- `resolveAPIKey` reads `${EnvPrefix}_AUTH_TOKEN` / `${EnvPrefix}_API_KEY`.
- `resolveModel` reads the host CLI's model env (Claude Code:
  `ANTHROPIC_DEFAULT_SONNET_MODEL`), falling back to `CERBERUS_LLM_MODEL` then
  the built-in default.

When `Profile.CLI == CLIUnknown`, each resolver keeps its **current
implementation unchanged** — `resolveBaseURL` reads `ANTHROPIC_BASE_URL`,
`resolveAPIKey` switches on model prefix, `resolveModel` reads
`CERBERUS_LLM_MODEL` then `ANTHROPIC_DEFAULT_SONNET_MODEL`. The prefix
parameterization applies only when a known CLI is detected, so standalone /
non-Claude-Code runs are byte-for-byte unchanged.

## §3 Provider resolution priority

Highest wins:

```
1. CERBERUS_LLM_PROVIDER            (explicit — user is certain)
2. detected CLI Provider            (Claude Code → "anthropic")   ← NEW hard fact
3. model-name prefix detectProvider (only when CLI unknown)
```

The model from `settings.json` / `resolveModel` selects **the model**, never the
provider. Under Claude Code, `glm-5.1` runs via the Anthropic protocol *because
it is Claude Code*, not because the prefix was unrecognized.

## §4 `internal/llm/` changes

`NewClientWithConfig` already receives `ClientConfig.Provider`. The change:
**if `Provider` is non-empty (set by the detection layer), trust it and do not
consult `detectProvider(model)`**. Only when `Provider` is empty (`CLIUnknown`)
does the model-name prefix fallback run. `detectProvider` itself is unchanged.

## §5 Testing

- `detect` package: `ClaudeCodeDetector` — `t.Setenv("CLAUDECODE","1")` hits;
  empty env misses. Two table cases cover the whole package.
- `config` integration: a priority table asserting `CERBERUS_LLM_PROVIDER` >
  detected CLI > settings model > model-name fallback, with each tier
  constructed via `t.Setenv`.
- No `/proc`, no process mocking — detection is a single env read, so the tests
  are platform-independent.

## §6 Extensibility

Adding Codex later = write `CodexDetector` (recognize `CODEX_*` / read
`~/.codex/`), append it to the detector registry. **No change to `config.Load`
or `internal/llm`** — that is the contract the interface buys.

## Out of Scope (YAGNI)

- Codex / Gemini CLI mappings — not verifiable without those CLIs running.
- Process-tree detection (`/proc` / `ps`) — platform-coupled, fragile in
  containers; the `CLAUDECODE` env is sufficient and cross-platform.
- Multi-signal voting / confidence scoring — one reliable signal beats a
  committee of fragile ones.

## Verification

- `make test` — new `detect` + `config` priority tests pass; existing tests
  green.
- `make lint` — new package passes `golangci-lint`.
- Manual: under Claude Code with `ANTHROPIC_*` + `ANTHROPIC_DEFAULT_SONNET_MODEL`
  set, `cerberus run` resolves provider `anthropic` with the `glm-5.1` model via
  the Anthropic-compatible endpoint (regression: no empty-content responses).

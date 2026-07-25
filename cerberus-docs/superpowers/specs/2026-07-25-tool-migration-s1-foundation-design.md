# Tool-Migration S1 — LLM Tool-Calling Foundation — Design — 2026-07-25

## Background

cerberus's LLM layer **already supports tool calling**: `ai.Driver.DecideWithTools`
(`internal/ai/driver_tools.go`), `Request.Tools` / `Response.ToolCalls`
(`internal/llm/types.go`), and all three providers (claude/gemini/openai) parse
`tool_use` / `functionCall` / `tool_calls`. But **no head uses `DecideWithTools`**
(grep confirms zero callers) — every head uses `Decide` structured output. This
dead infrastructure is the root cause of GLM drift and blocks the tool-calling
migration (approach A). **GATE PASS**: `TestDecideWithTools_LiveGLM`
(`internal/ai/driver_tools_live_test.go`) confirmed BigModel GLM emits
`tool_use` (`get_weather {location:Tokyo}`).

S1 is the foundation that unblocks S2-S4 (heads migration): make the mock client
support tool responses (so head-migration tests can preset `DecideWithTools`
results) and add provider tool-parse unit tests (currently zero coverage — the
parsing code exists but is untested).

## Goal

1. mock client: preset `tool_use` responses (backward-compatible).
2. provider tool-parse unit tests (claude/gemini/openai).

No head changes (S2-S4), no provider implementation changes (already done).

## Design

### 1. Mock client tool support (`internal/llm/mock.go`)

Add a tool-response path alongside the existing text path:

```go
type MockClient struct {
    responses     map[string]string
    toolResponses map[string][]ToolCall // NEW
}

// SetToolResponse presets a tool_use response for a given prompt key.
func (m *MockClient) SetToolResponse(key string, calls []ToolCall) {
    if m.toolResponses == nil {
        m.toolResponses = map[string][]ToolCall{}
    }
    m.toolResponses[key] = calls
}
```

**`matchKey` MUST consult `responses ∪ toolResponses`** (review-critical: today
`matchKey` only checks `responses`; a key present only in `toolResponses` would
hash to `sha256[:8]` and the setter would silently never match):

```go
func (m *MockClient) matchKey(input string) string {
    if _, ok := m.responses[input]; ok {
        return input
    }
    if _, ok := m.toolResponses[input]; ok {
        return input
    }
    return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))[:8]
}
```

`Complete` returns a tool response when the matched key is in `toolResponses`
(`Content=""`, `StopReason="tool_use"`, matching the GLM gate observation of a
pure `tool_use` turn); the existing text path is otherwise unchanged:

```go
func (m *MockClient) Complete(ctx context.Context, req Request) (*Response, error) {
    key := m.matchKey(joinedContent)
    if calls, ok := m.toolResponses[key]; ok {
        return &Response{Content: "", ToolCalls: calls, StopReason: "tool_use",
            Usage: TokenUsage{InputTokens: len(req.Messages) * 10, OutputTokens: 0,
                TotalTokens: len(req.Messages) * 10}}, nil
    }
    // existing text-response path unchanged
}
```

`NewMockClient(map[string]string)` signature is unchanged (zero ripple on
existing mock tests).

### 2. Provider tool-parse unit tests

claude (`tool_use`), gemini (`functionCall`), openai (`tool_calls`): each spins
up an `httptest.Server` returning a fixture response carrying a tool call, points
the provider at it, and asserts `Response.ToolCalls` parses correctly (name +
input). Follow the httptest pattern in `claude_stream_delta_test.go`. Today
provider tool parsing has **zero** test coverage (grep confirmed).

### 3. Mock self-test

`SetToolResponse` → `ai.Driver.DecideWithTools` returns the preset `ToolCalls`.

## Out of Scope

Head migration (S2 Scout, S3 Agent/Steer, S4 Examiner). Provider implementation
(already done — S1 only adds tests). Live tests (gate already passed).

## Testing

- mock: `SetToolResponse` → `Complete` returns `ToolCalls` (`Content=""`,
  `StopReason="tool_use"`); existing text path unchanged; `matchKey` consults
  both maps.
- provider: 3 httptest fixture tests (claude/gemini/openai) → `ToolCalls` parsed.
- regression: existing mock_test + provider tests unchanged.

## Constraints

Go 1.25 pure-Go; `coder/websocket` v1.8.14 only (untouched); no
expression/evaluator deps; author `binoctal <binoctal@gmail.com>`, no
Co-Authored-By; English; docs only in `cerberus-docs/`; `make check` green.

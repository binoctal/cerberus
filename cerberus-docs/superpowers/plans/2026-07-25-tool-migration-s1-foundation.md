# Tool-Migration S1 — LLM Tool-Calling Foundation — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cerberus's mock client support preset `tool_use` responses, and add provider tool-parse unit tests (claude/gemini/openai) — the foundation that unblocks S2-S4 heads migration to `DecideWithTools`.

**Architecture:** mock client gains a `toolResponses` map + `SetToolResponse` setter (backward-compatible); provider tool parsing (already implemented, zero test coverage) gets httptest fixture tests. No head changes, no provider impl changes.

**Tech Stack:** Go 1.25, `internal/llm` (mock + providers), `internal/ai` (Driver), `net/http/httptest`, testify.

## Global Constraints

- Go 1.25, pure-Go (no CGo); module `github.com/binoctal/cerberus`
- `coder/websocket` v1.8.14 only (untouched — no WS change)
- No expression/evaluator deps
- Commit author MUST be `binoctal <binoctal@gmail.com>`, NO Co-Authored-By trailer
- Code comments + commit messages in English
- Docs only in `cerberus-docs/`
- `make check` (fmt + lint + test -race) must be EXIT 0
- No provider implementation changes (parsing already done — S1 only adds tests)

---

### Task 1: Mock client tool-response support

**Files:**
- Modify: `internal/llm/mock.go` (MockClient struct, matchKey, Complete)
- Test: `internal/llm/mock_test.go` (append tool tests)

**Interfaces:**
- Consumes: `llm.ToolCall` (existing), `llm.Request`, `llm.Response`.
- Produces: `(*MockClient) SetToolResponse(key string, calls []ToolCall)`; `Complete` returns `ToolCalls` when the matched key is in `toolResponses`.

- [ ] **Step 1: Write the failing tests (RED)**

Append to `internal/llm/mock_test.go`:

```go
func TestMockClient_SetToolResponse(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("plan a relay", []ToolCall{
		{ID: "call_1", Name: "ws_relay", Input: map[string]any{"roles": []any{"web", "bridge"}}},
	})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "plan a relay"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content: got %q, want empty (pure tool_use turn)", resp.Content)
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("StopReason: got %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "ws_relay" {
		t.Fatalf("ToolCalls: got %+v, want one ws_relay call", resp.ToolCalls)
	}
}

// matchKey must consult toolResponses too — a key present only there must NOT
// hash to sha256[:8] (which would make SetToolResponse silently never match).
func TestMockClient_MatchKeyConsultsToolResponses(t *testing.T) {
	mock := NewMockClient(nil)
	mock.SetToolResponse("only-in-tool-responses", []ToolCall{{Name: "f"}})
	resp, err := mock.Complete(context.Background(), Request{
		Messages: []Message{{Role: "user", Content: "only-in-tool-responses"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("toolResponses-only key must match directly (no hash); got ToolCalls=%+v", resp.ToolCalls)
	}
}
```

Add `"context"` to the test file's imports if not present.

- [ ] **Step 2: Run tests to verify RED**

Run: `go test ./internal/llm/ -run 'TestMockClient_SetToolResponse|TestMockClient_MatchKeyConsultsToolResponses' -v`
Expected: COMPILE ERROR — `undefined: SetToolResponse` (or FAIL with empty ToolCalls).

- [ ] **Step 3: Add the `toolResponses` field + `SetToolResponse` setter**

In `internal/llm/mock.go`, change the struct + add the setter:

```go
type MockClient struct {
	responses     map[string]string
	toolResponses map[string][]ToolCall
}

// SetToolResponse presets a tool_use response for a given prompt key. When
// Complete matches this key it returns the tool calls (Content empty,
// StopReason "tool_use") instead of a text response.
func (m *MockClient) SetToolResponse(key string, calls []ToolCall) {
	if m.toolResponses == nil {
		m.toolResponses = map[string][]ToolCall{}
	}
	m.toolResponses[key] = calls
}
```

- [ ] **Step 4: Make `matchKey` consult `responses ∪ toolResponses`**

Replace `matchKey`:

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

- [ ] **Step 5: Return tool responses from `Complete`**

In `Complete`, right after computing `key := m.matchKey(...)`, add the tool branch before the existing text logic:

```go
	if calls, ok := m.toolResponses[key]; ok {
		return &Response{
			Content:    "",
			ToolCalls:  calls,
			StopReason: "tool_use",
			Usage: TokenUsage{
				InputTokens:  len(req.Messages) * 10,
				OutputTokens: 0,
				TotalTokens:  len(req.Messages) * 10,
			},
		}, nil
	}
```

The existing text-response path (default fallback) stays unchanged below it.

- [ ] **Step 6: Run tests to verify GREEN**

Run: `go test ./internal/llm/ -run 'TestMockClient_' -v`
Expected: PASS (both new tests + existing `TestMockClient_Stream*`).

- [ ] **Step 7: Commit**

```bash
git add internal/llm/mock.go internal/llm/mock_test.go
git commit -m "feat(llm): mock client tool-response support (SetToolResponse)

MockClient gains a toolResponses map + SetToolResponse setter. matchKey now
consults responses ∪ toolResponses so a toolResponses-only key matches directly
(else it would sha256-hash and the setter would silently never fire). Complete
returns a tool_use response (Content empty, StopReason tool_use) on a tool key.
Backward-compatible: NewMockClient(map[string]string) unchanged."
```

---

### Task 2: Provider tool-parse unit tests (claude/gemini/openai)

**Files:**
- Test (new): `internal/llm/provider_tool_parse_test.go`

**Interfaces:**
- Consumes: `llm.NewClientWithConfig(llm.ClientConfig{Provider, BaseURL, APIKey, Model})` → `Client`; `Client.Complete(ctx, req)`.
- Produces: three httptest fixture tests asserting `Response.ToolCalls`.

- [ ] **Step 1: Write the three fixture tests (RED — they pass once fixtures are correct, but lock the parsing contract)**

Create `internal/llm/provider_tool_parse_test.go`:

```go
package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// toolFixtureServer returns a provider-shaped JSON body for every request.
func toolFixtureServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func TestClaudeComplete_ToolUseParse(t *testing.T) {
	body := `{"content":[{"type":"tool_use","id":"call_1","name":"get_weather","input":{"location":"Tokyo"}}],"stop_reason":"tool_use","usage":{"input_tokens":5,"output_tokens":7}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "anthropic", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}

func TestGeminiComplete_FunctionCallParse(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"get_weather","args":{"location":"Tokyo"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":7,"totalTokenCount":12}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "gemini", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}

func TestOpenAIComplete_ToolCallsParse(t *testing.T) {
	body := `{"choices":[{"message":{"content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"location\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`
	srv := toolFixtureServer(t, body)
	defer srv.Close()
	c, err := NewClientWithConfig(ClientConfig{Provider: "openai", BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	resp, err := c.Complete(context.Background(), Request{Messages: []Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "get_weather" {
		t.Fatalf("ToolCalls = %+v, want one get_weather", resp.ToolCalls)
	}
	if got := resp.ToolCalls[0].Input["location"]; got != "Tokyo" {
		t.Errorf("input.location = %v, want Tokyo", got)
	}
}
```

- [ ] **Step 2: Run tests**

Run: `go test ./internal/llm/ -run 'TestClaudeComplete_ToolUseParse|TestGeminiComplete_FunctionCallParse|TestOpenAIComplete_ToolCallsParse' -v`
Expected: PASS (the parsing is already implemented; these lock the contract). If any FAIL, the fixture JSON shape is wrong — fix the fixture to match the provider's actual response struct (see `claude_complete_helpers.go`, `gemini_complete.go:68-86`, `openai_complete.go:58-80`).

- [ ] **Step 3: make check**

Run: `make check`
Expected: EXIT 0.

- [ ] **Step 4: Commit**

```bash
git add internal/llm/provider_tool_parse_test.go
git commit -m "test(llm): provider tool-parse coverage (claude/gemini/openai)

Add httptest fixture tests for the three providers' tool-call parsing
(claude tool_use, gemini functionCall, openai tool_calls -> ToolCalls),
which had zero coverage despite the parsing being implemented."
```

---

### Task 3: ai.Driver.DecideWithTools mock self-test

**Files:**
- Test: `internal/ai/driver_test.go` (append next to existing `TestDriver_DecideWithTools`)

**Interfaces:**
- Consumes: `llm.NewMockClient`, `(*MockClient).SetToolResponse` (Task 1), `ai.NewDriver`, `ai.NewTokenBudget`.
- Produces: a test confirming `DecideWithTools` surfaces a mock-preset `ToolCall`.

- [ ] **Step 1: Write the test**

Append to `internal/ai/driver_test.go` (near `TestDriver_DecideWithTools`):

```go
func TestDriver_DecideWithTools_MockPreset(t *testing.T) {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("plan a relay", []llm.ToolCall{
		{ID: "call_1", Name: "ws_relay", Input: map[string]any{"roles": []any{"web", "bridge"}}},
	})
	driver := NewDriver(mock, NewTokenBudget(10000, 2000))
	res, err := driver.DecideWithTools(context.Background(), "plan a relay", []llm.Tool{
		{Name: "ws_relay", Description: "emit a relay intent", InputSchema: map[string]any{"type": "object"}},
	})
	if err != nil {
		t.Fatalf("DecideWithTools: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "ws_relay" {
		t.Fatalf("ToolCalls = %+v, want one ws_relay", res.ToolCalls)
	}
}
```

Ensure `internal/ai/driver_test.go` imports `"context"` and `"github.com/binoctal/cerberus/internal/llm"` (add if missing; the existing `TestDriver_DecideWithTools` likely already imports both — match its import block).

- [ ] **Step 2: Run the test**

Run: `go test ./internal/ai/ -run TestDriver_DecideWithTools_MockPreset -v`
Expected: PASS.

- [ ] **Step 3: Full make check + commit**

Run: `make check`
Expected: EXIT 0.

```bash
git add internal/ai/driver_test.go
git commit -m "test(ai): DecideWithTools surfaces mock-preset tool calls

Confirm Driver.DecideWithTools returns a mock-preset ToolCall (via
MockClient.SetToolResponse), the pattern S2-S4 head-migration tests will use."
```

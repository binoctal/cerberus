package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
)

// RepairInput is one Examiner-diagnosed failure handed to Scout for targeted
// re-planning (feature #3). Hint+Reasoning guide the corrected case; Case is the
// original (its ID becomes the replacement's Replaces).
type RepairInput struct {
	Case      agent.TestCase
	Hint      agent.RedispatchHint
	Reasoning string
}

// RepairPlan asks the LLM for one corrected case per failure (feature #3). Each
// emitted repair_case tool call becomes a TestCase with Replaces set to its
// originating failure's ID. Degrades on LLM error: returns (nil, err) so the
// repair loop can log+break without aborting the run.
func (s *Scout) RepairPlan(ctx context.Context, goal string, failures []RepairInput) ([]agent.TestCase, error) {
	if len(failures) == 0 {
		return nil, nil
	}
	prompt := s.buildRepairPrompt(goal, failures)
	res, err := s.driver.DecideWithTools(ctx, prompt, repairTools())
	if err != nil {
		return nil, fmt.Errorf("repair plan: %w", err)
	}
	return assembleRepair(res.ToolCalls, failures), nil
}

// assembleRepair maps repair_case tool calls to replacement TestCases, pairing
// each to its originating failure via Replaces. One replacement per failure
// (first emission wins); an emission whose `replaces` matches no failure is
// dropped, as is any failure with no matching emission. Iterates repair_case
// calls in emission order; one replacement per failure (first emission wins).
// Output order follows the LLM's emission order, not the failures slice.
func assembleRepair(calls []llm.ToolCall, failures []RepairInput) []agent.TestCase {
	byID := make(map[string]int, len(failures))
	for i, f := range failures {
		byID[f.Case.ID] = i
	}
	used := make(map[int]bool, len(failures))
	var out []agent.TestCase
	for _, call := range calls {
		if call.Name != "repair_case" {
			continue
		}
		id := llm.StrField(call, "replaces")
		idx, ok := byID[id]
		if !ok || used[idx] {
			continue
		}
		used[idx] = true
		out = append(out, repairCaseFromCall(call, id))
	}
	return out
}

// repairCaseFromCall builds the corrected TestCase from a repair_case emission.
// When the call carries a `steps` array (a WebSocket flow), it builds a WS case
// (Action=ws_flow, Steps) mirroring Scout's plan assembly; otherwise it builds
// the HTTP shape (Target/Method/Body). Replaces binds it to the failed case.
func repairCaseFromCall(call llm.ToolCall, replaces string) agent.TestCase {
	if steps := parseRepairSteps(call); len(steps) > 0 {
		return agent.TestCase{
			ID:          fmt.Sprintf("repair-%s", replaces),
			Name:        fmt.Sprintf("repair %s", replaces),
			Action:      "ws_flow",
			Target:      wsFlowTarget(steps),
			Service:     llm.StrField(call, "service"),
			Steps:       steps,
			Expectation: llm.StrField(call, "expectation"),
			Replaces:    replaces,
		}
	}
	return agent.TestCase{
		ID:          fmt.Sprintf("repair-%s", replaces),
		Name:        fmt.Sprintf("repair %s", llm.StrField(call, "path")),
		Target:      llm.StrField(call, "path"),
		Method:      llm.StrField(call, "method"),
		Service:     llm.StrField(call, "service"),
		Body:        llm.StrField(call, "body"),
		Expectation: llm.StrField(call, "expectation"),
		Replaces:    replaces,
	}
}

// parseRepairSteps reads the optional `steps` array from a repair_case call into
// []TestStep. Returns nil when absent (HTTP repair).
func parseRepairSteps(call llm.ToolCall) []agent.TestStep {
	arr, ok := call.Input["steps"].([]any)
	if !ok {
		return nil
	}
	out := make([]agent.TestStep, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, agent.TestStep{
			Action:       mapStr(m, "action"),
			ConnectionID: mapStr(m, "connection_id"),
			Role:         mapStr(m, "role"),
			URL:          mapStr(m, "url"),
			Message:      mapStr(m, "message"),
			Type:         mapStr(m, "type"),
			Aliases:      mapStrSlice(m, "aliases"),
			Asserts:      mapAny(m, "asserts"),
			MatchAll:     mapBool(m, "match_all"),
			Timeout:      mapInt(m, "timeout"),
		})
	}
	return out
}

// wsFlowTarget returns the first step URL (the dial target), else "".
func wsFlowTarget(steps []agent.TestStep) string {
	for _, s := range steps {
		if s.URL != "" {
			return s.URL
		}
	}
	return ""
}

// map* helpers read typed fields from a step's map[string]any (mirroring the
// llm.ToolCall field helpers, which take a ToolCall rather than a raw map).
func mapStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func mapInt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}
func mapBool(m map[string]any, k string) bool {
	v, _ := m[k].(bool)
	return v
}
func mapStrSlice(m map[string]any, k string) []string {
	arr, ok := m[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
func mapAny(m map[string]any, k string) map[string]any {
	v, ok := m[k].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// repairTools returns the tool surface for RepairPlan: one repair_case call per
// failed case, carrying the corrected fields plus `replaces` to bind it back.
func repairTools() []llm.Tool {
	return []llm.Tool{
		{
			Name:        "repair_case",
			Description: "Emit a corrected test case that replaces one failed case. One call per failed case. For an HTTP case use method/path/service/body/expectation; for a WebSocket case use `steps` (the corrected ws_connect/ws_send/ws_receive/ws_disconnect flow) and leave the HTTP fields empty.",
			InputSchema: llm.ObjSchema([]any{"replaces"}, map[string]any{
				"replaces":    map[string]any{"type": "string", "description": "ID of the failed case this replaces"},
				"method":      map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"service":     map[string]any{"type": "string"},
				"body":        map[string]any{"type": "string"},
				"expectation": map[string]any{"type": "string"},
				"steps": map[string]any{
					"type":        "array",
					"description": "WebSocket flow: the corrected steps (omit for HTTP cases).",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"action":        map[string]any{"type": "string", "enum": []any{"ws_connect", "ws_send", "ws_receive", "ws_disconnect"}},
							"connection_id": map[string]any{"type": "string"},
							"role":          map[string]any{"type": "string"},
							"url":           map[string]any{"type": "string"},
							"message":       map[string]any{"type": "string"},
							"type":          map[string]any{"type": "string"},
							"aliases":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"asserts":       map[string]any{"type": "object"},
							"match_all":     map[string]any{"type": "boolean"},
							"timeout":       map[string]any{"type": "integer"},
						},
					},
				},
			}),
		},
	}
}

// buildRepairPrompt assembles the repair prompt: goal, per-failure context
// (original case + diagnosis), and the tool guide. Mirrors runAIPlanning's
// ai.NewPrompt() usage.
func (s *Scout) buildRepairPrompt(goal string, failures []RepairInput) string {
	var b []byte
	b = append(b, fmt.Sprintf("Goal: %s\n\nYou are repairing failed test cases. For EACH failed case below, emit ONE repair_case tool call with the corrected fields (set `replaces` to the failed case's ID). Only change what the diagnosis indicates; keep the rest.\n\n", goal)...)
	for i, f := range failures {
		b = append(b, fmt.Sprintf("## Failure %d (replaces=%s)\n- target: %s %s (service=%s)\n- body: %q\n- expectation: %s\n- diagnosis hint: %s\n- reasoning: %s\n\n",
			i+1, f.Case.ID, f.Case.Method, f.Case.Target, f.Case.Service,
			f.Case.Body, f.Case.Expectation, f.Hint, f.Reasoning)...)
	}
	return ai.NewPrompt().
		System(promptRepairSystem).
		Context(string(b)).
		Task("Emit one repair_case tool call per failed case above.").
		Output(promptRepairToolGuide).
		Build()
}

const promptRepairSystem = `You are a test-repair agent. Given failed cases with an Examiner diagnosis, emit exactly one corrected test case per failure via the repair_case tool. Correct only what the diagnosis indicates (wrong path/method = endpoint_drift; credentials = auth; payload = shape). Set ` + "`replaces`" + ` to the failed case's ID.`

const promptRepairToolGuide = `Emit ONE repair_case TOOL CALL PER FAILED CASE. Do not output JSON.`

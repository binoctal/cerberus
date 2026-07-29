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
// dropped, as is any failure with no matching emission. Iterates failures in
// input order for deterministic output.
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
// Target/Service/Method/Body carry the correction; Replaces binds it to the
// failed case. A deterministic ID makes traces/reporting readable.
func repairCaseFromCall(call llm.ToolCall, replaces string) agent.TestCase {
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

// repairTools returns the tool surface for RepairPlan: one repair_case call per
// failed case, carrying the corrected fields plus `replaces` to bind it back.
func repairTools() []llm.Tool {
	return []llm.Tool{
		{
			Name:        "repair_case",
			Description: "Emit a corrected test case that replaces one failed case. One call per failed case.",
			InputSchema: llm.ObjSchema([]any{"replaces", "method", "path"}, map[string]any{
				"replaces":    map[string]any{"type": "string", "description": "ID of the failed case this replaces"},
				"method":      map[string]any{"type": "string"},
				"path":        map[string]any{"type": "string"},
				"service":     map[string]any{"type": "string"},
				"body":        map[string]any{"type": "string"},
				"expectation": map[string]any{"type": "string"},
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

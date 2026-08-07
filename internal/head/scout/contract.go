package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

const promptContractSystem = `You define a coverage contract: what a test session must cover and how deeply. Given a project model, goal, and depth tier, return the scope, path types, error scopes, boundaries, priorities, and an objective coverage gate.`

// BuildCoverageContract creates an AI-authored coverage contract for a test session.
// The LLM is driven via six typed contract tools (declare_scope/path_types/
// error_scope/boundaries, set_priority, set_coverage_gate); assembleContract
// turns the calls into a contract.Contract. Invariants from the project
// configuration are carried into the contract as hard refs.
func (s *Scout) BuildCoverageContract(ctx context.Context, goal string, model *project.ProjectModel, depth string) (*contract.Contract, error) {
	dims := contract.ExpandDepth(depth)
	prompt := ai.NewPrompt().
		System(promptContractSystem).
		Context(s.buildAnalyzeContext(TargetInfo{Goal: goal})).
		Task(fmt.Sprintf("Goal: %s\nDepth: %s\nExpand to dimensions: %+v\nDefine the coverage contract via tools.", goal, depth, dims)).
		Build()
	res, err := s.driver.DecideWithTools(ctx, prompt, contractTools())
	if err != nil {
		return nil, fmt.Errorf("build coverage contract: %w", err)
	}
	if len(res.ToolCalls) == 0 {
		return nil, fmt.Errorf("build coverage contract: no tool calls")
	}
	var invs []contract.InvariantRef
	for _, inv := range s.config.Invariants {
		invs = append(invs, contract.InvariantRef{ID: inv.ID, Description: inv.Description})
	}
	return assembleContract(res.ToolCalls, depth, invs, servicesHaveVocab(s.config.Services)), nil
}

// servicesHaveVocab reports whether any service declares a non-empty WS
// vocabulary. Mirrors session.sessionHasVocab so Scout can pick the objective
// path gate for SaaS/WS contracts without importing the session package.
func servicesHaveVocab(services []project.Service) bool {
	for _, svc := range services {
		if svc.Vocabulary != nil && len(svc.Vocabulary.Edges) > 0 {
			return true
		}
	}
	return false
}

// SelfAssessContract critiques a coverage contract for gaps: missing scope,
// missing path types vs the depth tier, missing invariants. The LLM is driven
// via the report_contract_gap tool; each call surfaces one gap note. Returns
// notes the builder can fold into case generation.
// v1: notes are diagnostic-only (logged). Future versions should feed them into Plan context
// so they affect case generation.
func (s *Scout) SelfAssessContract(ctx context.Context, c *contract.Contract) ([]string, error) {
	prompt := ai.NewPrompt().
		System(`You critique a coverage contract for gaps: missing scope, missing path types vs the depth tier, missing invariants. Report each gap via report_contract_gap.`).
		Task(fmt.Sprintf("Contract: %+v", c)).
		Build()
	res, err := s.driver.DecideWithTools(ctx, prompt, selfAssessTools())
	if err != nil {
		return nil, fmt.Errorf("self-assess contract: %w", err)
	}
	// Zero report_contract_gap calls = LLM found no gaps; notes stay empty. This
	// is intentionally not an error (unlike BuildCoverageContract's zero-calls
	// path): self-assess is advisory, absence of gaps is a valid verdict.
	var notes []string
	for _, call := range res.ToolCalls {
		if call.Name == "report_contract_gap" {
			notes = append(notes, llm.StrField(call, "note"))
		}
	}
	return notes, nil
}

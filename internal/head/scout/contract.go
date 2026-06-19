package scout

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/project"
)

const promptContractSystem = `You define a coverage contract: what a test session must cover and how deeply. Given a project model, goal, and depth tier, return the scope, path types, error scopes, boundaries, priorities, and an objective coverage gate.`

// BuildCoverageContract creates an AI-authored coverage contract for a test session.
// It uses the Scout's driver to call the LLM, parses the response into a contract.Contract,
// and carries invariants from the project configuration into the contract.
func (s *Scout) BuildCoverageContract(ctx context.Context, goal string, model *project.ProjectModel, depth string) (*contract.Contract, error) {
	dims := contract.ExpandDepth(depth)
	prompt := ai.NewPrompt().
		System(promptContractSystem).
		Context(s.buildAnalyzeContext(TargetInfo{Goal: goal})).
		Task(fmt.Sprintf("Goal: %s\nDepth: %s\nExpand to dimensions: %+v\nReturn a JSON coverage contract.", goal, depth, dims)).
		Output(`Respond with JSON: {"depth":"","scope":[],"path_types":[],"error_scope":[],"boundaries":[],"priorities":{},"coverage_gate":{"module":"","line_threshold":0.0}}`).
		Build()

	var c contract.Contract
	if err := s.driver.Decide(ctx, prompt, &c); err != nil {
		return nil, fmt.Errorf("build coverage contract: %w", err)
	}
	if c.Depth == "" {
		c.Depth = depth
	}
	// carry invariants from config as hard refs
	for _, inv := range s.config.Invariants {
		c.Invariants = append(c.Invariants, contract.InvariantRef{ID: inv.ID, Description: inv.Description})
	}
	return &c, nil
}

// SelfAssessContract critiques a coverage contract for gaps: missing scope,
// missing path types vs the depth tier, missing invariants. Returns notes
// the builder can fold into case generation.
func (s *Scout) SelfAssessContract(ctx context.Context, c *contract.Contract) ([]string, error) {
	prompt := ai.NewPrompt().
		System(`You critique a coverage contract for gaps: missing scope, missing path types vs the depth tier, missing invariants. Return notes only.`).
		Task(fmt.Sprintf("Contract: %+v", c)).
		Output(`Respond with JSON: {"notes":[]}`).
		Build()
	var out struct{ Notes []string }
	if err := s.driver.Decide(ctx, prompt, &out); err != nil {
		return nil, fmt.Errorf("self-assess contract: %w", err)
	}
	return out.Notes, nil
}

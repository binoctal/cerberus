package ai

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
)

// CodeGenerator generates test code from scenarios
type CodeGenerator struct {
	llmClient llm.Client
}

// GeneratedTest represents generated test code with metadata
type GeneratedTest struct {
	Code       string
	Language   string
	FilePath   string
	LineNumber int
}

// NewCodeGenerator creates a new code generator
func NewCodeGenerator(llmClient llm.Client) *CodeGenerator {
	return &CodeGenerator{
		llmClient: llmClient,
	}
}

// GenerateTestCode generates test code for a scenario
func (g *CodeGenerator) GenerateTestCode(scenario *Scenario, funcInfo *FuncInfo) (string, error) {
	// Stub - will be implemented with LLM in later tasks
	return g.generateStubCode(scenario, funcInfo), nil
}

// generateStubCode generates basic test structure (temporary implementation)
func (g *CodeGenerator) generateStubCode(scenario *Scenario, funcInfo *FuncInfo) string {
	switch funcInfo.Language {
	case "go":
		return g.generateGoStub(scenario, funcInfo)
	case "python":
		return g.generatePythonStub(scenario, funcInfo)
	case "javascript":
		return g.generateJavaScriptStub(scenario, funcInfo)
	default:
		return fmt.Sprintf("// Unsupported language: %s", funcInfo.Language)
	}
}

// generateGoStub generates Go test stub
func (g *CodeGenerator) generateGoStub(scenario *Scenario, funcInfo *FuncInfo) string {
	return fmt.Sprintf(`func %s(t *testing.T) {
	// %s
	// TODO: Implement test logic
}`, scenario.Name, scenario.Description)
}

// generatePythonStub generates Python test stub
func (g *CodeGenerator) generatePythonStub(scenario *Scenario, funcInfo *FuncInfo) string {
	return fmt.Sprintf(`def test_%s():
	"""
	%s
	"""
	# TODO: Implement test logic
	pass`, scenario.Name, scenario.Description)
}

// generateJavaScriptStub generates JavaScript test stub
func (g *CodeGenerator) generateJavaScriptStub(scenario *Scenario, funcInfo *FuncInfo) string {
	return fmt.Sprintf(`test('%s', () => {
	// %s
	// TODO: Implement test logic
});`, scenario.Name, scenario.Description)
}

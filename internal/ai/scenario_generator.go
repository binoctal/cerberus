package ai

import (
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/pkg/business"
)

// ScenarioGenerator generates test scenarios
type ScenarioGenerator struct {
	llmClient     llm.Client
	businessModel *business.BusinessModel
}

// Scenario represents a test scenario
type Scenario struct {
	Name        string
	Description string
	Input       map[string]interface{}
	Expected    map[string]interface{}
	Rationale   string
	Type        string // "normal" | "edge" | "error" | "combination"
}

// FuncInfo contains function analysis results
type FuncInfo struct {
	Name       string
	Parameters []Parameter
	ReturnType string
	Logic      string
	FilePath   string
	Package    string
	Language   string
}

// Parameter represents a function parameter
type Parameter struct {
	Name string
	Type string
}

// NewScenarioGenerator creates a new scenario generator
func NewScenarioGenerator(llmClient llm.Client, businessModel *business.BusinessModel) *ScenarioGenerator {
	return &ScenarioGenerator{
		llmClient:     llmClient,
		businessModel: businessModel,
	}
}

// GenerateScenarios generates all test scenarios for a function
func (g *ScenarioGenerator) GenerateScenarios(funcName string, rules []business.BusinessRule) []Scenario {
	var scenarios []Scenario

	// 1. Normal scenarios
	normal := g.generateNormalScenarios(funcName, rules)
	scenarios = append(scenarios, normal...)

	// 2. Edge scenarios
	edge := g.generateEdgeScenarios(funcName, rules)
	scenarios = append(scenarios, edge...)

	// 3. Error scenarios
	errors := g.generateErrorScenarios(funcName, rules)
	scenarios = append(scenarios, errors...)

	// 4. Combination scenarios
	combinations := g.generateCombinations(funcName, rules)
	scenarios = append(scenarios, combinations...)

	return scenarios
}

// generateNormalScenarios generates normal business scenarios
func (g *ScenarioGenerator) generateNormalScenarios(funcName string, rules []business.BusinessRule) []Scenario {
	// Stub - will be implemented with LLM in later tasks
	return []Scenario{
		{
			Name:        "NormalScenario",
			Description: "Normal business flow",
			Type:        "normal",
		},
	}
}

// generateEdgeScenarios generates edge case scenarios
func (g *ScenarioGenerator) generateEdgeScenarios(funcName string, rules []business.BusinessRule) []Scenario {
	// Stub - will be implemented with LLM in later tasks
	return []Scenario{
		{
			Name:        "EdgeScenario",
			Description: "Edge case scenario",
			Type:        "edge",
		},
	}
}

// generateErrorScenarios generates error handling scenarios
func (g *ScenarioGenerator) generateErrorScenarios(funcName string, rules []business.BusinessRule) []Scenario {
	// Stub - will be implemented with LLM in later tasks
	return []Scenario{
		{
			Name:        "ErrorScenario",
			Description: "Error handling scenario",
			Type:        "error",
		},
	}
}

// generateCombinations generates business rule combination scenarios
func (g *ScenarioGenerator) generateCombinations(funcName string, rules []business.BusinessRule) []Scenario {
	// Stub - will be implemented with LLM in later tasks
	return []Scenario{
		{
			Name:        "CombinationScenario",
			Description: "Rule combination scenario",
			Type:        "combination",
		},
	}
}

package business

import (
	"fmt"
	"time"
)

// BusinessModel represents AI's understanding of project business logic
type BusinessModel struct {
	// Metadata
	ID              string
	ProjectPath     string
	GeneratedAt     time.Time
	Version         int

	// AI inference confidence
	Confidence      float64  // 0.0-1.0, overall confidence
	InferenceSource string  // "ai_autonomous" | "ai_assisted" | "manual"

	// Business domain identification
	Domain           string  // "e-commerce", "finance", "social-network"
	DomainConfidence float64  // Domain identification confidence

	// Core business concepts
	Concepts []BusinessConcept

	// Business rules
	Rules []BusinessRule

	// Business workflows
	Workflows []Workflow

	// Invariants and constraints
	Invariants []Invariant

	// Edge cases (AI inferred)
	EdgeCases []EdgeCase

	// Error scenarios
	ErrorScenarios []ErrorScenario
}

// BusinessConcept represents a business entity or concept
type BusinessConcept struct {
	Name        string
	Type        string  // "entity", "value_object", "service"
	Description string
	RelatedTo   []string
	Inferred    bool    // AI inferred or user-provided
	Confidence  float64 // Inference confidence
}

// BusinessRule represents a business logic rule
type BusinessRule struct {
	ID          string
	Name        string
	Description string
	Condition   string
	Effect      string
	Priority    string  // "critical", "high", "medium", "low"
	Examples    []string
	Source      string  // "comment" | "code_pattern" | "inferred"
	Confidence  float64 // Rule inference confidence
}

// Workflow represents a business process flow
type Workflow struct {
	Name        string
	Steps       []WorkflowStep
	EntryPoints []string
	ExitPoints  []string
	Inferred    bool
}

type WorkflowStep struct {
	Name        string
	Description string
	NextSteps   []string
}

// Invariant represents a business constraint
type Invariant struct {
	Description string
	Expression  string
	Scope       string
	Source      string  // "execution_observed" | "inferred"
}

// EdgeCase represents a boundary scenario
type EdgeCase struct {
	Name        string
	Description string
	Trigger     string
	Expected    string
	Rationale   string
	Confidence  float64
}

// ErrorScenario represents an error handling scenario
type ErrorScenario struct {
	Name     string
	Trigger  string
	Handling string
	Recovery string
}

// Validate checks if the business model is valid
func (m *BusinessModel) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("business model ID cannot be empty")
	}

	if m.Confidence < 0 || m.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got: %f", m.Confidence)
	}

	if m.Domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}

	if m.DomainConfidence < 0 || m.DomainConfidence > 1 {
		return fmt.Errorf("domain confidence must be between 0 and 1, got: %f", m.DomainConfidence)
	}

	return nil
}

// CalculateOverallConfidence computes overall confidence from components
func (m *BusinessModel) CalculateOverallConfidence() float64 {
	var avgConceptConf, avgRuleConf float64

	if len(m.Concepts) > 0 {
		conceptSum := 0.0
		for _, c := range m.Concepts {
			conceptSum += c.Confidence
		}
		avgConceptConf = conceptSum / float64(len(m.Concepts))
	}

	if len(m.Rules) > 0 {
		ruleSum := 0.0
		for _, r := range m.Rules {
			ruleSum += r.Confidence
		}
		avgRuleConf = ruleSum / float64(len(m.Rules))
	}

	// If both empty, return 0
	if avgConceptConf == 0 && avgRuleConf == 0 {
		return 0.0
	}

	// Weight: rules 60%, concepts 40%
	return (avgRuleConf * 0.6) + (avgConceptConf * 0.4)
}

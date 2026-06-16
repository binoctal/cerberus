package ai

import (
	"encoding/json"
	"fmt"
)

// PatternType represents different types of patterns
type PatternType int

const (
	BusinessPatterns PatternType = iota
	DomainPattern
	WorkflowPattern
	StateMachinePattern
	RulePattern
	EdgeCasePattern
	ErrorHandlingPattern
)

// Pattern represents a recognized business or architectural pattern
type Pattern struct {
	ID          string
	Type        PatternType
	Name        string
	Description string
	Locations   []PatternLocation
	Confidence  float64
	Metadata    map[string]interface{}
}

// PatternLocation represents where a pattern appears in code
type PatternLocation struct {
	FilePath    string
	LineNumber  int
	Context     string
	Signature   string // Function/struct signature
}

// PatternDatabase stores known patterns and their characteristics
type PatternDatabase struct {
	BusinessPatterns    []*Pattern
	DomainPatterns      []*Pattern
	WorkflowPatterns    []*Pattern
	StateMachinePatterns []*Pattern
	RulePatterns        []*Pattern
	EdgeCasePatterns    []*Pattern
	ErrorHandlingPatterns []*Pattern
}

// PatternRecognizer identifies business and architectural patterns in code
type PatternRecognizer struct {
	database *PatternDatabase
}

// NewPatternRecognizer creates a new pattern recognizer
func NewPatternRecognizer() *PatternRecognizer {
	return &PatternRecognizer{
		database: &PatternDatabase{
			BusinessPatterns:     []*Pattern{},
			DomainPatterns:       []*Pattern{},
			WorkflowPatterns:     []*Pattern{},
			StateMachinePatterns: []*Pattern{},
			RulePatterns:         []*Pattern{},
			EdgeCasePatterns:     []*Pattern{},
			ErrorHandlingPatterns: []*Pattern{},
		},
	}
}

// RecognizeBusinessPatterns identifies business-relevant patterns in code
// This method:
// 1. Analyzes code structure and comments
// 2. Matches against known business patterns
// 3. Identifies domain-specific patterns
// 4. Returns patterns with confidence scores
func (pr *PatternRecognizer) RecognizeBusinessPatterns(code string, comments []*Comment) ([]*Pattern, error) {
	// Stub implementation - will be fully implemented in later tasks
	// For now, return empty slice
	return []*Pattern{}, nil
}

// recognizeStateMachines identifies state machine patterns in code
func (pr *PatternRecognizer) recognizeStateMachines(code string) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// recognizeWorkflows identifies workflow patterns in code
func (pr *PatternRecognizer) recognizeWorkflows(code string) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// recognizeBusinessRules identifies business rule patterns in code
func (pr *PatternRecognizer) recognizeBusinessRules(code string, comments []*Comment) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// recognizeEdgeCases identifies edge case handling patterns
func (pr *PatternRecognizer) recognizeEdgeCases(code string) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// recognizeErrorHandling identifies error handling patterns
func (pr *PatternRecognizer) recognizeErrorHandling(code string) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// recognizeDomainPatterns identifies domain-specific patterns
func (pr *PatternRecognizer) recognizeDomainPatterns(code string, comments []*Comment) ([]*Pattern, error) {
	// Stub implementation
	return []*Pattern{}, nil
}

// calculatePatternConfidence calculates confidence score for a recognized pattern
func (pr *PatternRecognizer) calculatePatternConfidence(pattern *Pattern) float64 {
	confidence := 0.5

	// Increase confidence based on number of locations
	if len(pattern.Locations) > 0 {
		confidence += 0.1
	}
	if len(pattern.Locations) > 2 {
		confidence += 0.1
	}

	// Increase confidence if pattern has clear description
	if pattern.Description != "" {
		confidence += 0.1
	}

	// Increase confidence if pattern has metadata
	if len(pattern.Metadata) > 0 {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// addPattern adds a pattern to the database
func (pr *PatternRecognizer) addPattern(pattern *Pattern) {
	switch pattern.Type {
	case BusinessPatterns:
		pr.database.BusinessPatterns = append(pr.database.BusinessPatterns, pattern)
	case DomainPattern:
		pr.database.DomainPatterns = append(pr.database.DomainPatterns, pattern)
	case WorkflowPattern:
		pr.database.WorkflowPatterns = append(pr.database.WorkflowPatterns, pattern)
	case StateMachinePattern:
		pr.database.StateMachinePatterns = append(pr.database.StateMachinePatterns, pattern)
	case RulePattern:
		pr.database.RulePatterns = append(pr.database.RulePatterns, pattern)
	case EdgeCasePattern:
		pr.database.EdgeCasePatterns = append(pr.database.EdgeCasePatterns, pattern)
	case ErrorHandlingPattern:
		pr.database.ErrorHandlingPatterns = append(pr.database.ErrorHandlingPatterns, pattern)
	}
}

// getPatternsByType retrieves all patterns of a specific type
func (pr *PatternRecognizer) getPatternsByType(patternType PatternType) []*Pattern {
	switch patternType {
	case BusinessPatterns:
		return pr.database.BusinessPatterns
	case DomainPattern:
		return pr.database.DomainPatterns
	case WorkflowPattern:
		return pr.database.WorkflowPatterns
	case StateMachinePattern:
		return pr.database.StateMachinePatterns
	case RulePattern:
		return pr.database.RulePatterns
	case EdgeCasePattern:
		return pr.database.EdgeCasePatterns
	case ErrorHandlingPattern:
		return pr.database.ErrorHandlingPatterns
	default:
		return []*Pattern{}
	}
}

// String returns the string representation of a pattern type
func (p PatternType) String() string {
	switch p {
	case BusinessPatterns:
		return "Business"
	case DomainPattern:
		return "Domain"
	case WorkflowPattern:
		return "Workflow"
	case StateMachinePattern:
		return "StateMachine"
	case RulePattern:
		return "Rule"
	case EdgeCasePattern:
		return "EdgeCase"
	case ErrorHandlingPattern:
		return "ErrorHandling"
	default:
		return "Unknown"
	}
}

// Validate checks if a pattern is valid and complete
func (p *Pattern) Validate() error {
	if p == nil {
		return fmt.Errorf("pattern is nil")
	}

	if p.ID == "" {
		return fmt.Errorf("pattern ID is empty")
	}

	if p.Name == "" {
		return fmt.Errorf("pattern name is empty")
	}

	if p.Type < BusinessPatterns || p.Type > ErrorHandlingPattern {
		return fmt.Errorf("invalid pattern type: %d", p.Type)
	}

	if p.Confidence < 0.0 || p.Confidence > 1.0 {
		return fmt.Errorf("confidence must be between 0.0 and 1.0, got %f", p.Confidence)
	}

	return nil
}

// ToJSON converts a pattern to JSON representation
func (p *Pattern) ToJSON() (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal pattern to JSON: %w", err)
	}
	return string(data), nil
}

// GetDatabase returns the pattern database
func (pr *PatternRecognizer) GetDatabase() *PatternDatabase {
	return pr.database
}

// GetPatternCount returns the total number of patterns in the database
func (pr *PatternRecognizer) GetPatternCount() int {
	return len(pr.database.BusinessPatterns) +
		len(pr.database.DomainPatterns) +
		len(pr.database.WorkflowPatterns) +
		len(pr.database.StateMachinePatterns) +
		len(pr.database.RulePatterns) +
		len(pr.database.EdgeCasePatterns) +
		len(pr.database.ErrorHandlingPatterns)
}

// ClearPatterns clears all patterns from the database
func (pr *PatternRecognizer) ClearPatterns() {
	pr.database = &PatternDatabase{
		BusinessPatterns:     []*Pattern{},
		DomainPatterns:       []*Pattern{},
		WorkflowPatterns:     []*Pattern{},
		StateMachinePatterns: []*Pattern{},
		RulePatterns:         []*Pattern{},
		EdgeCasePatterns:     []*Pattern{},
		ErrorHandlingPatterns: []*Pattern{},
	}
}

package ai

import "fmt"

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
	BusinessPatterns      []*Pattern
	DomainPatterns        []*Pattern
	WorkflowPatterns      []*Pattern
	StateMachinePatterns []*Pattern
	RulePatterns          []*Pattern
	EdgeCasePatterns      []*Pattern
	ErrorHandlingPatterns []*Pattern
}

// String returns the string representation of PatternType
func (p PatternType) String() string {
	switch p {
	case BusinessPatterns:
		return "business"
	case DomainPattern:
		return "domain"
	case WorkflowPattern:
		return "workflow"
	case StateMachinePattern:
		return "state_machine"
	case RulePattern:
		return "rule"
	case EdgeCasePattern:
		return "edge_case"
	case ErrorHandlingPattern:
		return "error_handling"
	default:
		return "unknown"
	}
}

// Validate checks if a pattern has all required fields
func (p *Pattern) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("pattern name is required")
	}
	if p.Type < BusinessPatterns || p.Type > ErrorHandlingPattern {
		return fmt.Errorf("invalid pattern type: %d", p.Type)
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("confidence must be between 0 and 1, got: %f", p.Confidence)
	}
	return nil
}

// ToJSON converts a pattern to JSON string representation
func (p *Pattern) ToJSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", fmt.Errorf("pattern validation failed: %w", err)
	}
	// Simplified JSON representation
	return fmt.Sprintf(`{"id":"%s","type":"%s","name":"%s","confidence":%.2f}`,
		p.ID, p.Type, p.Name, p.Confidence), nil
}

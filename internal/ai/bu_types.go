// internal/ai/bu_types.go
package ai

import (
	"fmt"
	"strings"
	"time"
)

// BusinessUnderstandingResult contains the complete analysis result.
type BusinessUnderstandingResult struct {
	ProjectPath   string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	CodeInsights  *CodeInsights
	Comments      []*Comment
	Patterns      []*Pattern
	BusinessModel *BusinessModel
	Questions     []*Question
	UserAnswers   map[string]string // Question ID -> Answer
	RefinedModel  *BusinessModel    // After user answers
	Confidence    float64           // Overall confidence score
}

// BusinessModel represents the inferred business model.
type BusinessModel struct {
	Domain        string
	Entities      []Entity
	Workflows     []Workflow
	BusinessRules []BusinessRule
	Constraints   []Constraint
	StateMachines []StateMachine
	EdgeCases     []EdgeCase
	APICount      int
	LayerCount    int
	CouplingScore float64
	Confidence    float64
	Assumptions   []string
	Gaps          []string
}

// Entity represents a business entity.
type Entity struct {
	Name          string
	Type          string
	Attributes    []Attribute
	Relationships []Relationship
}

// Attribute represents an entity attribute.
type Attribute struct {
	Name        string
	Type        string
	Required    bool
	Constraints []string
}

// Relationship represents an entity relationship.
type Relationship struct {
	Type         string
	Target       string
	Multiplicity string
}

// Workflow represents a business workflow.
type Workflow struct {
	Name       string
	Steps      []string
	Conditions []string
	Exceptions []string
}

// BusinessRule represents a business rule.
type BusinessRule struct {
	Name        string
	Description string
	Conditions  []string
	Actions     []string
	Priority    string
}

// Constraint represents a business constraint.
type Constraint struct {
	Name        string
	Type        string
	Description string
	Expression  string
}

// EdgeCase represents an edge case.
type EdgeCase struct {
	Name        string
	Description string
	Handling    string
}

// String returns a string representation of the business model.
func (bm *BusinessModel) String() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Domain: %s\n", bm.Domain)
	fmt.Fprintf(&sb, "Entities: %d\n", len(bm.Entities))
	fmt.Fprintf(&sb, "Workflows: %d\n", len(bm.Workflows))
	fmt.Fprintf(&sb, "Business Rules: %d\n", len(bm.BusinessRules))
	fmt.Fprintf(&sb, "Constraints: %d\n", len(bm.Constraints))
	fmt.Fprintf(&sb, "State Machines: %d\n", len(bm.StateMachines))
	fmt.Fprintf(&sb, "Edge Cases: %d\n", len(bm.EdgeCases))
	fmt.Fprintf(&sb, "Confidence: %.2f\n", bm.Confidence)

	return sb.String()
}

// Validate checks if a business understanding result is valid.
func (bur *BusinessUnderstandingResult) Validate() error {
	if bur == nil {
		return fmt.Errorf("business understanding result is nil")
	}

	if bur.ProjectPath == "" {
		return fmt.Errorf("project path is empty")
	}

	if bur.CodeInsights == nil {
		return fmt.Errorf("code insights is nil")
	}

	if bur.BusinessModel == nil {
		return fmt.Errorf("business model is nil")
	}

	return nil
}

// getFilePaths returns all file paths from code insights.
func (ci *CodeInsights) getFilePaths() []string {
	var paths []string
	for _, module := range ci.Modules {
		paths = append(paths, module.FilePaths...)
	}
	return paths
}

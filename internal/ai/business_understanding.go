package ai

import (
	"fmt"
	"strings"
	"time"

	"github.com/binoctal/cerberus/internal/llm"
)

// BusinessUnderstandingEngine orchestrates the AI quality framework
// This is the main orchestrator that coordinates all components
type BusinessUnderstandingEngine struct {
	codeAnalyzer       *CodeAnalyzer
	commentMiner       *CommentMiner
	patternRecognizer  *PatternRecognizer
	interactionController *MinimalInteraction
	llmClient          llm.Client
}

// NewBusinessUnderstandingEngine creates a new business understanding engine
func NewBusinessUnderstandingEngine(llmClient llm.Client) *BusinessUnderstandingEngine {
	return &BusinessUnderstandingEngine{
		codeAnalyzer:       NewCodeAnalyzer(llmClient),
		commentMiner:       NewCommentMiner(),
		patternRecognizer:  NewPatternRecognizer(),
		interactionController: NewMinimalInteraction(InteractionConfig{
			ConfidenceThreshold: 0.7,
			MaxQuestions:         5,
			BusinessCriticalOnly: true,
		}),
		llmClient:          llmClient,
	}
}

// UnderstandProject performs comprehensive business understanding analysis
// This is the main orchestration method with 6-phase flow:
// Phase 1: Code Analysis - Analyze code structure and dependencies
// Phase 2: Comment Mining - Extract business-relevant comments
// Phase 3: Pattern Recognition - Identify business and architectural patterns
// Phase 4: AI Inference - Use LLM to infer business model and constraints
// Phase 5: Minimal Interaction - Ask critical questions only
// Phase 6: Save/Display - Persist and display results
func (bue *BusinessUnderstandingEngine) UnderstandProject(projectPath string) (*BusinessUnderstandingResult, error) {
	result := &BusinessUnderstandingResult{
		ProjectPath: projectPath,
		StartTime:   time.Now(),
	}

	// Phase 1: Code Analysis
	insights, err := bue.codeAnalyzer.AnalyzeDeeply(projectPath)
	if err != nil {
		return nil, fmt.Errorf("code analysis failed: %w", err)
	}
	result.CodeInsights = insights

	// Phase 2: Comment Mining
	comments, err := bue.commentMiner.MineAggressively(projectPath, insights.getFilePaths())
	if err != nil {
		return nil, fmt.Errorf("comment mining failed: %w", err)
	}
	result.Comments = comments

	// Phase 3: Pattern Recognition
	patterns, err := bue.patternRecognizer.RecognizeBusinessPatterns("", comments)
	if err != nil {
		return nil, fmt.Errorf("pattern recognition failed: %w", err)
	}
	result.Patterns = patterns

	// Phase 4: AI Inference
	businessModel, err := bue.inferBusinessModel(insights, comments, patterns)
	if err != nil {
		return nil, fmt.Errorf("AI inference failed: %w", err)
	}
	result.BusinessModel = businessModel

	// Phase 5: Minimal Interaction
	questions := bue.interactionController.GenerateCriticalQuestionsOnly(insights, comments, patterns)
	result.Questions = questions

	// Phase 6: Save and Display
	if err := bue.saveAndDisplay(result); err != nil {
		return nil, fmt.Errorf("save and display failed: %w", err)
	}

	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// BusinessUnderstandingResult contains the complete analysis result
type BusinessUnderstandingResult struct {
	ProjectPath    string
	StartTime      time.Time
	EndTime        time.Time
	Duration       time.Duration
	CodeInsights   *CodeInsights
	Comments       []*Comment
	Patterns       []*Pattern
	BusinessModel  *BusinessModel
	Questions      []*Question
	UserAnswers    map[string]string // Question ID -> Answer
	RefinedModel   *BusinessModel     // After user answers
	Confidence     float64            // Overall confidence score
}

// BusinessModel represents the inferred business model
type BusinessModel struct {
	Domain           string
	Entities         []Entity
	Workflows        []Workflow
	BusinessRules    []BusinessRule
	Constraints      []Constraint
	StateMachines    []StateMachine
	EdgeCases        []EdgeCase
	APICount         int
	LayerCount       int
	CouplingScore    float64
	Confidence       float64
	Assumptions      []string
	Gaps             []string
}

// Entity represents a business entity
type Entity struct {
	Name        string
	Type        string
	Attributes  []Attribute
	Relationships []Relationship
}

// Attribute represents an entity attribute
type Attribute struct {
	Name     string
	Type     string
	Required bool
	Constraints []string
}

// Relationship represents an entity relationship
type Relationship struct {
	Type   string
	Target string
	Multiplicity string
}

// Workflow represents a business workflow
type Workflow struct {
	Name        string
	Steps       []string
	Conditions  []string
	Exceptions  []string
}

// BusinessRule represents a business rule
type BusinessRule struct {
	Name        string
	Description string
	Conditions  []string
	Actions     []string
	Priority    string
}

// Constraint represents a business constraint
type Constraint struct {
	Name        string
	Type        string
	Description string
	Expression  string
}

// EdgeCase represents an edge case
type EdgeCase struct {
	Name        string
	Description string
	Handling    string
}

// inferBusinessModel uses AI to infer the business model from code and comments
// This is Phase 4 of the orchestration flow
func (bue *BusinessUnderstandingEngine) inferBusinessModel(insights *CodeInsights, comments []*Comment, patterns []*Pattern) (*BusinessModel, error) {
	model := &BusinessModel{
		Entities:      []Entity{},
		Workflows:     []Workflow{},
		BusinessRules: []BusinessRule{},
		Constraints:   []Constraint{},
		StateMachines: []StateMachine{},
		EdgeCases:     []EdgeCase{},
		Assumptions:   []string{},
		Gaps:          []string{},
	}

	// Infer domain from patterns and comments
	model.Domain = bue.inferDomain(comments, patterns)

	// Extract entities from code insights
	model.Entities = bue.extractEntities(insights)

	// Extract workflows from patterns
	model.Workflows = bue.extractWorkflows(patterns)

	// Extract business rules from comments
	model.BusinessRules = bue.extractBusinessRules(comments)

	// Extract constraints
	model.Constraints = bue.extractConstraints(insights, comments)

	// Extract state machines
	model.StateMachines = insights.StateMachines

	// Extract edge cases
	model.EdgeCases = bue.extractEdgeCases(comments)

	// Calculate metrics
	model.APICount = len(insights.APIContracts)
	model.LayerCount = len(insights.Layers)
	if insights.Coupling != nil {
		model.CouplingScore = insights.Coupling.AverageCoupling
	}

	// Calculate overall confidence
	model.Confidence = bue.calculateModelConfidence(model, comments, patterns)

	// Identify assumptions
	model.Assumptions = bue.identifyAssumptions(comments, patterns)

	// Identify gaps
	model.Gaps = bue.identifyGaps(insights, comments, patterns)

	return model, nil
}

// askMinimalQuestions asks critical questions to the user
// This is part of Phase 5 of the orchestration flow
func (bue *BusinessUnderstandingEngine) askMinimalQuestions(result *BusinessUnderstandingResult) error {
	if len(result.Questions) == 0 {
		return nil
	}

	// In a real implementation, this would interact with the user
	// For now, just record that questions were asked
	return nil
}

// refineWithAnswers refines the business model with user answers
// This continues Phase 5 after user provides answers
func (bue *BusinessUnderstandingEngine) refineWithAnswers(result *BusinessUnderstandingResult, answers map[string]string) (*BusinessModel, error) {
	result.UserAnswers = answers

	// Create refined model based on answers
	refinedModel := &BusinessModel{
		Domain:         result.BusinessModel.Domain,
		Entities:       result.BusinessModel.Entities,
		Workflows:      result.BusinessModel.Workflows,
		BusinessRules:  result.BusinessModel.BusinessRules,
		Constraints:    result.BusinessModel.Constraints,
		StateMachines:  result.BusinessModel.StateMachines,
		EdgeCases:      result.BusinessModel.EdgeCases,
		APICount:       result.BusinessModel.APICount,
		LayerCount:     result.BusinessModel.LayerCount,
		CouplingScore:  result.BusinessModel.CouplingScore,
		Confidence:     result.BusinessModel.Confidence + 0.1, // Increase confidence with user input
		Assumptions:    []string{},
		Gaps:           []string{},
	}

	// Update assumptions and gaps based on answers
	for questionID, answer := range answers {
		// Process answer and update model
		refinedModel.Assumptions = append(refinedModel.Assumptions, fmt.Sprintf("Q: %s, A: %s", questionID, answer))
	}

	result.RefinedModel = refinedModel
	return refinedModel, nil
}

// saveAndDisplay saves and displays the analysis results
// This is Phase 6 of the orchestration flow
func (bue *BusinessUnderstandingEngine) saveAndDisplay(result *BusinessUnderstandingResult) error {
	// In a real implementation, this would save to database or display to user
	// For now, just validate the result
	if result == nil {
		return fmt.Errorf("result is nil")
	}

	return nil
}

// Helper methods for business model inference

func (bue *BusinessUnderstandingEngine) inferDomain(comments []*Comment, patterns []*Pattern) string {
	// Simple heuristic: use most common business term
	termCounts := make(map[string]int)

	for _, comment := range comments {
		if comment.Semantics != nil && comment.Semantics.BusinessTerm != "" {
			termCounts[comment.Semantics.BusinessTerm]++
		}
	}

	var domain string
	maxCount := 0
	for term, count := range termCounts {
		if count > maxCount {
			maxCount = count
			domain = term
		}
	}

	if domain == "" {
		domain = "unknown"
	}

	return domain
}

func (bue *BusinessUnderstandingEngine) extractEntities(insights *CodeInsights) []Entity {
	// Stub implementation
	return []Entity{}
}

func (bue *BusinessUnderstandingEngine) extractWorkflows(patterns []*Pattern) []Workflow {
	// Stub implementation
	return []Workflow{}
}

func (bue *BusinessUnderstandingEngine) extractBusinessRules(comments []*Comment) []BusinessRule {
	var rules []BusinessRule

	for _, comment := range comments {
		if comment.Semantics != nil && comment.Semantics.Purpose == "business_rule" {
			rule := BusinessRule{
				Name:        fmt.Sprintf("Rule from %s:%d", comment.FilePath, comment.LineNumber),
				Description: comment.Text,
				Priority:    "medium",
			}
			rules = append(rules, rule)
		}
	}

	return rules
}

func (bue *BusinessUnderstandingEngine) extractConstraints(insights *CodeInsights, comments []*Comment) []Constraint {
	// Stub implementation
	return []Constraint{}
}

func (bue *BusinessUnderstandingEngine) extractEdgeCases(comments []*Comment) []EdgeCase {
	// Stub implementation
	return []EdgeCase{}
}

func (bue *BusinessUnderstandingEngine) calculateModelConfidence(model *BusinessModel, comments []*Comment, patterns []*Pattern) float64 {
	confidence := 0.5

	// Increase confidence based on data completeness
	if len(model.Entities) > 0 {
		confidence += 0.1
	}
	if len(model.Workflows) > 0 {
		confidence += 0.1
	}
	if len(model.BusinessRules) > 0 {
		confidence += 0.1
	}
	if len(model.Constraints) > 0 {
		confidence += 0.1
	}

	// Increase confidence based on comment and pattern quality
	highConfidenceComments := 0
	for _, comment := range comments {
		if comment.Semantics != nil && comment.Semantics.Confidence > 0.7 {
			highConfidenceComments++
		}
	}

	if highConfidenceComments > len(comments)/2 {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (bue *BusinessUnderstandingEngine) identifyAssumptions(comments []*Comment, patterns []*Pattern) []string {
	// Stub implementation
	return []string{}
}

func (bue *BusinessUnderstandingEngine) identifyGaps(insights *CodeInsights, comments []*Comment, patterns []*Pattern) []string {
	// Stub implementation
	return []string{}
}

// getFilePaths returns all file paths from code insights
func (ci *CodeInsights) getFilePaths() []string {
	var paths []string
	for _, module := range ci.Modules {
		paths = append(paths, module.FilePaths...)
	}
	return paths
}

// GetCodeAnalyzer returns the code analyzer
func (bue *BusinessUnderstandingEngine) GetCodeAnalyzer() *CodeAnalyzer {
	return bue.codeAnalyzer
}

// GetCommentMiner returns the comment miner
func (bue *BusinessUnderstandingEngine) GetCommentMiner() *CommentMiner {
	return bue.commentMiner
}

// GetPatternRecognizer returns the pattern recognizer
func (bue *BusinessUnderstandingEngine) GetPatternRecognizer() *PatternRecognizer {
	return bue.patternRecognizer
}

// GetInteractionController returns the interaction controller
func (bue *BusinessUnderstandingEngine) GetInteractionController() *MinimalInteraction {
	return bue.interactionController
}

// String returns a string representation of the business model
func (bm *BusinessModel) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Domain: %s\n", bm.Domain))
	sb.WriteString(fmt.Sprintf("Entities: %d\n", len(bm.Entities)))
	sb.WriteString(fmt.Sprintf("Workflows: %d\n", len(bm.Workflows)))
	sb.WriteString(fmt.Sprintf("Business Rules: %d\n", len(bm.BusinessRules)))
	sb.WriteString(fmt.Sprintf("Constraints: %d\n", len(bm.Constraints)))
	sb.WriteString(fmt.Sprintf("State Machines: %d\n", len(bm.StateMachines)))
	sb.WriteString(fmt.Sprintf("Edge Cases: %d\n", len(bm.EdgeCases)))
	sb.WriteString(fmt.Sprintf("Confidence: %.2f\n", bm.Confidence))

	return sb.String()
}

// Validate checks if a business understanding result is valid
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

package ai

import (
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/llm"
)

// BusinessUnderstandingEngine orchestrates the AI quality framework.
// This is the main orchestrator that coordinates all components.
type BusinessUnderstandingEngine struct {
	codeAnalyzer          *CodeAnalyzer
	commentMiner          *CommentMiner
	patternRecognizer     *PatternRecognizer
	interactionController *MinimalInteraction
	llmClient             llm.Client
}

// NewBusinessUnderstandingEngine creates a new business understanding engine.
func NewBusinessUnderstandingEngine(llmClient llm.Client) *BusinessUnderstandingEngine {
	return &BusinessUnderstandingEngine{
		codeAnalyzer:      NewCodeAnalyzer(llmClient),
		commentMiner:      NewCommentMiner(),
		patternRecognizer: NewPatternRecognizer(),
		interactionController: NewMinimalInteraction(InteractionConfig{
			ConfidenceThreshold:  0.7,
			MaxQuestions:         5,
			BusinessCriticalOnly: true,
		}),
		llmClient: llmClient,
	}
}

// UnderstandProject performs comprehensive business understanding analysis.
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

// inferBusinessModel uses AI to infer the business model from code and comments.
// This is Phase 4 of the orchestration flow.
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

// GetCodeAnalyzer returns the code analyzer.
func (bue *BusinessUnderstandingEngine) GetCodeAnalyzer() *CodeAnalyzer {
	return bue.codeAnalyzer
}

// GetCommentMiner returns the comment miner.
func (bue *BusinessUnderstandingEngine) GetCommentMiner() *CommentMiner {
	return bue.commentMiner
}

// GetPatternRecognizer returns the pattern recognizer.
func (bue *BusinessUnderstandingEngine) GetPatternRecognizer() *PatternRecognizer {
	return bue.patternRecognizer
}

// GetInteractionController returns the interaction controller.
func (bue *BusinessUnderstandingEngine) GetInteractionController() *MinimalInteraction {
	return bue.interactionController
}

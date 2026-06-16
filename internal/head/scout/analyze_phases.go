package scout

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/project"
)

// analysisScope holds the state for project model analysis.
type analysisScope struct {
	ctx          context.Context
	target       TargetInfo
	configModel  *project.ProjectModel
	aiPrompt     string
	aiOutput     AnalyzeOutput
	shouldSkipAI bool
}

// buildConfigModel creates the initial model from project configuration.
func (s *Scout) buildConfigModel() *project.ProjectModel {
	return s.buildModelFromConfig()
}

// checkModelCoverage determines if AI inference is needed based on info score.
func (s *Scout) checkModelCoverage(model *project.ProjectModel) bool {
	if model.InfoScore(false) >= 0.7 {
		s.logger.Info("model has sufficient coverage from config, skipping AI inference",
			zap.Float64("info_score", model.InfoScore(false)),
		)
		return true
	}
	return false
}

// buildAIPrompt constructs the full AI prompt for analysis.
func (s *Scout) buildAIPrompt(target TargetInfo) string {
	analyzeCtx := s.buildAnalyzeContext(target)
	return ai.NewPrompt().
		System(promptAnalyzeSystem).
		Context(analyzeCtx).
		Task(s.buildAnalyzeTask(target)).
		Output(promptAnalyzeOutput).
		Build()
}

// runAIInference executes AI analysis with graceful degradation.
func (s *Scout) runAIInference(ctx context.Context, prompt string, configModel *project.ProjectModel) (*project.ProjectModel, error) {
	var out AnalyzeOutput
	if err := s.driver.Decide(ctx, prompt, &out); err != nil {
		s.logger.Warn("AI analysis failed, using config-only model", zap.Error(err))
		return configModel, nil // Graceful degradation
	}

	// Merge AI-inferred data into the model
	model := &project.ProjectModel{}
	*model = *configModel // Copy config model
	s.mergeAIInference(model, out)

	return model, nil
}

// finalizeAnalysis logs the final model statistics.
func (s *Scout) finalizeAnalysis(model *project.ProjectModel) {
	s.logger.Info("project model built",
		zap.Int("endpoints", len(model.API.Endpoints)),
		zap.Int("pages", len(model.Navigation.Pages)),
		zap.Float64("info_score", model.InfoScore(false)),
	)
}

// Analyze builds a ProjectModel from the target info using AI inference
// and the project configuration as ground truth.
func (s *Scout) Analyze(ctx context.Context, target TargetInfo) (*project.ProjectModel, error) {
	// Phase 1: Build model from config
	model := s.buildConfigModel()

	// Phase 2: Check if AI inference is needed
	if s.checkModelCoverage(model) {
		return model, nil
	}

	// Phase 3: Build AI prompt
	prompt := s.buildAIPrompt(target)

	// Phase 4: Run AI inference with merge
	model, err := s.runAIInference(ctx, prompt, model)
	if err != nil {
		return model, err
	}

	// Phase 5: Log final statistics
	s.finalizeAnalysis(model)

	return model, nil
}

// buildModelFromConfig creates a ProjectModel from the project YAML config.
func (s *Scout) buildModelFromConfig() *project.ProjectModel {
	model := &project.ProjectModel{}

	// Add endpoints from config invariants (which reference specific paths).
	for _, inv := range s.config.Invariants {
		model.InvariantHints = append(model.InvariantHints, project.InvariantHint{
			ID:          inv.ID,
			Source:      "config",
			Description: inv.Description,
			Confidence:  0.95, // Explicit config = high confidence.
			Severity:    inv.Severity,
		})
	}

	// Add known endpoints from service health checks.
	for _, svc := range s.config.Services {
		if svc.Health != "" {
			model.API.Endpoints = append(model.API.Endpoints, project.EndpointDef{
				Method:     "GET",
				Path:       svc.Health,
				Confidence: 0.95,
			})
		}
	}

	return model
}

// buildAnalyzeTask constructs the analysis task prompt.
// When URL is empty (local-only mode), the Base URL line is omitted
// so the LLM skips HTTP test case generation.
func (s *Scout) buildAnalyzeTask(target TargetInfo) string {
	if target.URL == "" {
		return fmt.Sprintf("Analyze this project and infer its testable surface.\nGoal: %s", target.Goal)
	}
	return fmt.Sprintf("Analyze this SaaS project and infer its API surface.\nBase URL: %s\nGoal: %s",
		target.URL, target.Goal)
}

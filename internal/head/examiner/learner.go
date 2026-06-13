package examiner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
	"go.uber.org/zap"
)

// Learner performs Reflexion: generates reflections from test results
// and stores them as L3 procedural memory.
type Learner struct {
	driver   *ai.Driver
	store    *store.Store
	logger   *zap.Logger
	embedder embedPkg.Provider
}

// NewLearner creates a Reflexion learner.
// If embedder is nil, a default TrigramProvider is used.
func NewLearner(driver *ai.Driver, s *store.Store, logger *zap.Logger, embedder embedPkg.Provider) *Learner {
	if embedder == nil {
		embedder = embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	}
	return &Learner{driver: driver, store: s, logger: logger, embedder: embedder}
}

// Learn generates reflections from all step results and stores them as L3 procedural memory.
// Returns the count of reflections stored (after quality gating).
func (l *Learner) Learn(ctx context.Context, input LearnInput) (int, error) {
	if len(input.Results) == 0 {
		return 0, nil
	}

	// Build reflection context from all results.
	reflectionCtx := l.buildReflectionContext(input.Results)

	prompt := ai.NewPrompt().
		System(promptReflectionSystem).
		Context(reflectionCtx).
		Task("Generate reflections for all test results above. Include both failure and success reflections.").
		Output(promptReflectionOutput).
		Build()

	var reflections []Reflection
	if err := l.driver.Decide(ctx, prompt, &reflections); err != nil {
		return 0, fmt.Errorf("reflection decide: %w", err)
	}

	// Quality gate and store.
	stored := 0
	for _, r := range reflections {
		if !qualityGate(r) {
			l.logger.Debug("reflection rejected by quality gate",
				zap.String("diagnosis", r.Diagnosis),
				zap.String("pattern", r.ConditionPattern),
			)
			continue
		}

		_, err := l.store.StoreProceduralWithType(ctx,
			r.Category,
			r.ConditionPattern,
			r.Strategy,
			input.Project,
			r.Category,
			r.Type,
		)
		if err != nil {
			l.logger.Warn("store reflection", zap.Error(err))
			continue
		}
		stored++
	}

	l.logger.Info("reflexion learning complete",
		zap.Int("total", len(reflections)),
		zap.Int("stored", stored),
	)

	// Store key facts as L2 semantic memory for future retrieval.
	l.storeSemanticFromReflections(ctx, reflections, input.Project)

	return stored, nil
}

// qualityGate validates a reflection before L3 storage.
func qualityGate(r Reflection) bool {
	if r.Diagnosis == "" {
		return false
	}
	if len(r.Strategy) < 10 {
		return false
	}
	if r.ConditionPattern == "" {
		return false
	}
	if r.Type != "failure" && r.Type != "success" {
		return false
	}
	return true
}

// buildReflectionContext formats all step results for the reflection prompt.
func (l *Learner) buildReflectionContext(results []agent.StepResult) string {
	var b strings.Builder
	b.WriteString("Test Results:\n\n")

	for i, r := range results {
		fmt.Fprintf(&b, "### Result %d: %s (%s)\n", i+1, r.TestCase.Name, r.TestCase.ID)
		fmt.Fprintf(&b, "Status: %s\n", r.Status)
		fmt.Fprintf(&b, "Target: %s\n", r.TestCase.Target)
		fmt.Fprintf(&b, "Expectation: %s\n", r.TestCase.Expectation)
		fmt.Fprintf(&b, "Attempts: %d\n", r.Attempts)

		if r.Result != nil {
			if httpRes, ok := r.Result.(types.HTTPResult); ok {
				if httpRes.StatusCode != 0 {
					fmt.Fprintf(&b, "HTTP Status: %d\n", httpRes.StatusCode)
				}
				if httpRes.Body != "" {
					body := httpRes.Body
					if len(body) > 500 {
						body = body[:500] + "..."
					}
					fmt.Fprintf(&b, "Response: %s\n", body)
				}
				if httpRes.Err != "" {
					fmt.Fprintf(&b, "Error: %s\n", httpRes.Err)
				}
			} else {
				fmt.Fprintf(&b, "Result: %s\n", r.Result.Summary())
			}
		}
		if r.Error != nil {
			fmt.Fprintf(&b, "Step Error: %s\n", r.Error)
		}

		// Include evidence if available.
		if len(r.Evidence) > 0 {
			evJSON, _ := json.Marshal(r.Evidence)
			fmt.Fprintf(&b, "Evidence: %s\n", string(evJSON))
		}

		b.WriteString("\n")
	}

	return b.String()
}

// storeSemanticFromReflections extracts key facts from reflections and stores
// them as L2 semantic memory with embeddings for future retrieval.
func (l *Learner) storeSemanticFromReflections(ctx context.Context, reflections []Reflection, project string) {
	for _, r := range reflections {
		if !qualityGate(r) {
			continue
		}
		content := fmt.Sprintf("%s: %s → %s", r.Type, r.Diagnosis, r.Strategy)
		embedding, err := l.embedder.Embed(ctx, content)
		if err != nil {
			l.logger.Warn("generate embedding", zap.Error(err))
			continue
		}

		_, storeErr := l.store.StoreSemantic(ctx, content, "reflexion", project,
			[]string{r.Category, r.Type}, embedding, l.embedder.ModelName())
		if storeErr != nil {
			l.logger.Warn("store semantic memory", zap.Error(storeErr))
		}
	}
}

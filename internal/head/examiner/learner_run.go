package examiner

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

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

package examiner

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/memory"
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
		Output(promptReflectionToolGuide).
		Build()

	// Learner site: DecideWithTools + assembleReflections. Error PROPAGATES
	// (wraps as "reflection decide: ...") — Reflexion is non-fatal background
	// learning, but a real LLM error should still surface so the caller can
	// log it (Examiner.Examine already treats Learn errors as non-fatal).
	// Zero tool calls degrade to empty reflections (NOT propagate): drift
	// produces nothing to learn, not a run-ending failure. The quality gate
	// below is unchanged — it filters assembled Reflections before L3 storage.
	res, err := l.driver.DecideWithTools(ctx, prompt, learnerTools())
	if err != nil {
		return 0, fmt.Errorf("reflection decide: %w", err)
	}
	reflections := assembleReflections(res.ToolCalls)

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

		cond := memory.NormalizeCondition(r.ConditionPattern)
		emb, embErr := l.embedder.Embed(ctx, cond)
		if embErr != nil {
			l.logger.Warn("embed condition failed", zap.Error(embErr))
			emb = nil
		}
		_, err := l.store.StoreProceduralWithType(ctx,
			r.Category,
			cond,
			r.Strategy,
			input.Project,
			r.Category,
			r.Type,
			emb, l.embedder.ModelName(),
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

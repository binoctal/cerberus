package examiner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

// Learner performs Reflexion: generates reflections from test results
// and stores them as L3 procedural memory.
type Learner struct {
	driver *ai.Driver
	store  *store.Store
	logger *zap.Logger
}

// NewLearner creates a Reflexion learner.
func NewLearner(driver *ai.Driver, s *store.Store, logger *zap.Logger) *Learner {
	return &Learner{driver: driver, store: s, logger: logger}
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
		b.WriteString(fmt.Sprintf("### Result %d: %s (%s)\n", i+1, r.TestCase.Name, r.TestCase.ID))
		b.WriteString(fmt.Sprintf("Status: %s\n", r.Status))
		b.WriteString(fmt.Sprintf("Target: %s\n", r.TestCase.Target))
		b.WriteString(fmt.Sprintf("Expectation: %s\n", r.TestCase.Expectation))
		b.WriteString(fmt.Sprintf("Attempts: %d\n", r.Attempts))

		if r.LastObs.StatusCode != 0 {
			b.WriteString(fmt.Sprintf("HTTP Status: %d\n", r.LastObs.StatusCode))
		}
		if r.LastObs.Body != "" {
			body := r.LastObs.Body
			if len(body) > 500 {
				body = body[:500] + "..."
			}
			b.WriteString(fmt.Sprintf("Response: %s\n", body))
		}
		if r.LastObs.Error != "" {
			b.WriteString(fmt.Sprintf("Error: %s\n", r.LastObs.Error))
		}
		if r.Error != nil {
			b.WriteString(fmt.Sprintf("Step Error: %s\n", r.Error))
		}

		// Include evidence if available.
		if len(r.Evidence) > 0 {
			evJSON, _ := json.Marshal(r.Evidence)
			b.WriteString(fmt.Sprintf("Evidence: %s\n", string(evJSON)))
		}

		b.WriteString("\n")
	}

	return b.String()
}

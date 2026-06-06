package agent

import (
	"context"
	"fmt"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

// Recovery handles failed actions by consulting the LLM with failure context
// and injecting relevant L3 procedural memory.
type Recovery struct {
	driver *ai.Driver
	store  *store.Store
	config ReActConfig
	logger *zap.Logger
}

// NewRecovery creates a Recovery decision point handler.
func NewRecovery(driver *ai.Driver, store *store.Store, config ReActConfig, logger *zap.Logger) *Recovery {
	return &Recovery{driver: driver, store: store, config: config, logger: logger}
}

// RecoverResult holds the recovery decision output.
type RecoverResult struct {
	Action Action
	Skip   bool
}

// Recover decides what to do after a failed action.
// Returns the next Action and whether to skip the step entirely.
func (rc *Recovery) Recover(ctx context.Context, tc TestCase, obs Observation, attempt int) (RecoverResult, error) {
	recoverCtx := rc.buildRecoverContext(ctx, tc, obs, attempt)

	prompt := ai.NewPrompt().
		System(promptRecoverSystem).
		Context(recoverCtx).
		Task(fmt.Sprintf("Failed action on target: %s\nError: %s\nAttempt: %d/%d",
			tc.Target, obs.Error, attempt, rc.config.MaxSteerAttempts)).
		Output(promptRecoverOutput).
		Build()

	var out RecoverOutput
	if err := rc.driver.Decide(ctx, prompt, &out); err != nil {
		rc.logger.Warn("recover parse failed, skipping", zap.Error(err))
		return RecoverResult{Skip: true}, nil
	}

	rc.logger.Info("recover decision",
		zap.String("diagnosis", out.Diagnosis),
		zap.Bool("skip", out.Skip),
		zap.String("action_type", string(out.Action.Type)),
	)

	return RecoverResult{Action: out.Action, Skip: out.Skip}, nil
}

// buildRecoverContext assembles context including L3 procedural memory.
func (rc *Recovery) buildRecoverContext(ctx context.Context, tc TestCase, obs Observation, attempt int) string {
	var b []byte

	// Current failure context
	b = append(b, fmt.Sprintf("Target: %s\nStatus Code: %d\nError: %s\n",
		tc.Target, obs.StatusCode, obs.Error)...)

	// L3 Procedural Memory injection
	memories, err := rc.store.GetProceduralByMatch(ctx, tc.Target, 5)
	if err != nil {
		rc.logger.Warn("failed to load L3 memory for recovery", zap.Error(err))
	}
	if len(memories) > 0 {
		b = append(b, "\n## Learned Strategies\n"...)
		for _, m := range memories {
			b = append(b, fmt.Sprintf("- When %s: %s (effectiveness: %.0f%%)\n",
				m.Condition, m.Action, m.Effectiveness*100)...)
		}
	}

	return string(b)
}

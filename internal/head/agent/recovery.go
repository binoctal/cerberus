package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// Recovery handles failed actions by consulting the LLM with failure context
// and injecting relevant L3 procedural memory.
type Recovery struct {
	driver    *ai.Driver
	store     *store.Store
	config    ReActConfig
	logger    *zap.Logger
	embedder  embed.Provider
	sessionID string
}

// NewRecovery creates a Recovery decision point handler.
func NewRecovery(driver *ai.Driver, store *store.Store, config ReActConfig, logger *zap.Logger, embedder embed.Provider) *Recovery {
	if embedder == nil {
		embedder = embed.NewTrigramProvider(embed.DefaultDimension)
	}
	return &Recovery{driver: driver, store: store, config: config, logger: logger, embedder: embedder}
}

// SetSessionID is called by the loop at the start of ExecutePlan so recovery can
// attribute memory_usage to the right session.
func (rc *Recovery) SetSessionID(id string) {
	rc.sessionID = id
}

// Recover decides what to do after a failed action.
// Returns a RecoverDecision with the next action or skip flag.
func (rc *Recovery) Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error) {
	recoverCtx := rc.buildRecoverContext(ctx, tc, result, attempt)

	prompt := ai.NewPrompt().
		System(promptRecoverSystem).
		Context(recoverCtx).
		Task(fmt.Sprintf("Failed action on target: %s\nError: %s\nAttempt: %d/%d",
			tc.Target, result.Summary(), attempt, rc.config.MaxSteerAttempts)).
		Output(promptRecoverOutput).
		Build()

	var out RecoverOutput
	if err := rc.driver.Decide(ctx, prompt, &out); err != nil {
		rc.logger.Warn("recover parse failed, skipping", zap.Error(err))
		return RecoverDecision{Skip: true}, nil
	}

	action, err := actionFromEnvelope(out.Envelope, tc.Target, rc.logger)
	if err != nil {
		rc.logger.Warn("recover action unmarshal failed, skipping", zap.Error(err))
		return RecoverDecision{Skip: true}, nil
	}

	rc.logger.Info("recover decision",
		zap.String("diagnosis", out.Diagnosis),
		zap.Bool("skip", out.Skip),
		zap.String("action_type", string(action.GetActionType())),
	)

	return RecoverDecision{Action: action, Skip: out.Skip}, nil
}

// buildRecoverContext assembles context including L3 procedural memory.
func (rc *Recovery) buildRecoverContext(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) string {
	var b []byte

	// Current failure context.
	b = append(b, fmt.Sprintf("Target: %s\nSummary: %s\nAttempt: %d\n",
		tc.Target, result.Summary(), attempt)...)

	// L3 Procedural Memory injection.
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

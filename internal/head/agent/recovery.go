package agent

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/memory"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

// Recovery handles failed actions by consulting the LLM with failure context
// and injecting relevant L3 procedural memory.
type Recovery struct {
	driver      *ai.Driver
	store       *store.Store
	config      ReActConfig
	logger      *zap.Logger
	embedder    embed.Provider
	sessionID   string
	projectName string
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

// SetProject is called by the loop to provide the project name for L3 recall filtering.
func (rc *Recovery) SetProject(name string) {
	rc.projectName = name
}

// Recover decides what to do after a failed action.
//
// Returns a RecoverDecision that is now mutually exclusive under S3's
// tool-calling path: {Skip: true} carries a nil Action (the case is abandoned
// — finalized as StepSkipped), while {Action: <assembled>} carries Skip=false
// (the loop's tryRecovery runs the recovered action). Pre-S3 the two fields
// could both be set (Skip short-circuited in tryRecovery); S3 makes the
// exclusivity explicit because the LLM now emits either an action tool call OR
// a `skip` tool call OR nothing at all — never both.
//
// Terminal states (all but the action-tool path collapse to Skip:true):
//   - transient LLM error (budget, network)  → RecoverDecision{Skip: true}, nil
//     err (graceful skip — pre-S3 behavior preserved)
//   - zero tool calls (drift)                → RecoverDecision{Skip: true}
//   - first tool call is `skip`              → RecoverDecision{Skip: true}
//   - first tool call is an action tool      → assembleAction →
//     RecoverDecision{Action: <assembled>, Skip: false}
//   - action assembly fails (should not happen post-schema) → RecoverDecision{Skip: true}
//
// The legacy `Diagnosis` field on RecoverOutput is gone — tool calls carry no
// diagnosis text. The trade-off (lost recovery rationale in logs) is accepted,
// paralleling S2's ToT `reasoning` removal.
func (rc *Recovery) Recover(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) (RecoverDecision, error) {
	recoverCtx := rc.buildRecoverContext(ctx, tc, result, attempt)

	prompt := ai.NewPrompt().
		System(promptRecoverSystem).
		Context(recoverCtx).
		Task(fmt.Sprintf("Failed action on target: %s\nError: %s\nAttempt: %d/%d\nEmit one action tool call to retry, or the `skip` tool to abandon this target.",
			tc.Target, result.Summary(), attempt, rc.config.MaxSteerAttempts)).
		Build()

	res, err := rc.driver.DecideWithTools(ctx, prompt, recoveryTools())
	if err != nil {
		rc.logger.Warn("recover call failed, skipping", zap.Error(err))
		return RecoverDecision{Skip: true}, nil
	}
	if len(res.ToolCalls) == 0 {
		rc.logger.Warn("recover: zero tool calls (drift), skipping")
		return RecoverDecision{Skip: true}, nil
	}
	first := res.ToolCalls[0]
	if first.Name == "skip" {
		rc.logger.Info("recover decision", zap.String("tool", "skip"))
		return RecoverDecision{Skip: true}, nil
	}
	action, aErr := assembleAction(first)
	if aErr != nil {
		rc.logger.Warn("recover: action assemble failed, skipping", zap.Error(aErr))
		return RecoverDecision{Skip: true}, nil
	}
	rc.logger.Info("recover decision",
		zap.String("tool", first.Name),
		zap.String("action_type", string(action.GetActionType())),
	)
	return RecoverDecision{Action: action}, nil
}

// buildRecoverContext assembles context including L3 procedural memory.
func (rc *Recovery) buildRecoverContext(ctx context.Context, tc TestCase, result types.ExecutorResult, attempt int) string {
	var b []byte
	b = append(b, fmt.Sprintf("Target: %s\nSummary: %s\nAttempt: %d\n",
		tc.Target, result.Summary(), attempt)...)

	memories := rc.recallProcedural(ctx, tc.Target)
	if len(memories) > 0 {
		b = append(b, "\n## Learned Strategies\n"...)
		for _, m := range memories {
			b = append(b, fmt.Sprintf("- When %s: %s (effectiveness: %.0f%%)\n",
				m.Condition, m.Action, m.Effectiveness*100)...)
			if rc.sessionID != "" {
				if err := rc.store.RecordMemoryUsage(ctx, m.ID, rc.sessionID, tc.ID, tc.Target, attempt); err != nil {
					rc.logger.Warn("record memory_usage failed", zap.Error(err))
				}
			}
		}
	}
	return string(b)
}

// recallProcedural retrieves relevant procedural memories using embedding search.
func (rc *Recovery) recallProcedural(ctx context.Context, target string) []store.ProceduralMemory {
	if rc.embedder == nil {
		return nil
	}
	q, err := rc.embedder.Embed(ctx, memory.NormalizeTarget(target))
	if err != nil {
		rc.logger.Warn("embed target failed", zap.Error(err))
		return nil
	}
	memories, err := rc.store.GetProceduralByEmbedding(ctx, q, rc.projectName, rc.recallTopK(), rc.recallThreshold(), rc.embedder.ModelName())
	if err != nil {
		rc.logger.Warn("procedural embedding recall failed", zap.Error(err))
		return nil
	}
	return memories
}

// recallTopK returns the configured L3 recall cap, falling back to the default
// when unset (e.g. configs constructed before the field existed).
func (rc *Recovery) recallTopK() int {
	if rc.config.ProceduralRecallTopK > 0 {
		return rc.config.ProceduralRecallTopK
	}
	return DefaultReActConfig().ProceduralRecallTopK
}

// recallThreshold returns the configured L3 cosine threshold, falling back to
// the default when unset.
func (rc *Recovery) recallThreshold() float64 {
	if rc.config.ProceduralRecallThreshold > 0 {
		return rc.config.ProceduralRecallThreshold
	}
	return DefaultReActConfig().ProceduralRecallThreshold
}

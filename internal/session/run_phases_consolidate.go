package session

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
	"go.uber.org/zap"
)

// executeConsolidatePhase runs after verdicts are committed. It is idempotent
// (safe on resume): episodic writes key on session+target+verdict, effectiveness
// EMA is guarded by memory_usage.consolidated_at, and Learn dedups via upsert.
func (rp *runPhase) executeConsolidatePhase() error {
	if err := rp.writeEpisodicMemory(); err != nil {
		rp.session.Logger.Warn("episodic consolidate failed", zap.Error(err))
	}
	// Effectiveness EMA + governance are added in later tasks; each degrades on error.
	return nil
}

func (rp *runPhase) writeEpisodicMemory() error {
	for _, v := range rp.verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Target == "" {
			continue
		}
		target := memory.NormalizeTarget(tc.Target)
		if err := rp.session.Store.RecordEpisodic(
			rp.ctx, rp.session.ID, target, string(v.Status), string(v.Status), v.StepResult.Duration); err != nil {
			rp.session.Logger.Warn("record episodic failed",
				zap.String("target", target), zap.Error(err))
		}
	}
	return nil
}

// ensure unused import guard for examiner stays valid if StatusX referenced.
var _ = examiner.StatusPass
var _ = fmt.Stringer(nil)

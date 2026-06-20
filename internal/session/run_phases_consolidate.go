package session

import (
	"context"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
)

// executeConsolidatePhase runs after verdicts are committed. It is idempotent
// (safe on resume): episodic writes key on session+target+verdict, effectiveness
// EMA is guarded by memory_usage.consolidated_at, and Learn dedups via upsert.
func (rp *runPhase) executeConsolidatePhase() error {
	if err := writeEpisodicMemory(rp.ctx, rp.session, rp.verdicts); err != nil {
		rp.session.Logger.Warn("episodic consolidate failed", zap.Error(err))
	}
	if err := applyEffectiveness(rp.ctx, rp.session, rp.verdicts); err != nil {
		rp.session.Logger.Warn("effectiveness consolidate failed", zap.Error(err))
	}
	if err := archiveStale(rp.ctx, rp.session); err != nil {
		rp.session.Logger.Warn("archive stale failed", zap.Error(err))
	}
	return nil
}

// writeEpisodicMemory records episodic memories for verdicts.
// Shared by both runPhase and resumePhase consolidate logic.
func writeEpisodicMemory(ctx context.Context, session *Session, verdicts []examiner.FinalVerdict) error {
	for _, v := range verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Target == "" {
			continue
		}
		target := memory.NormalizeTarget(tc.Target)
		if err := session.Store.RecordEpisodic(
			ctx, session.ID, target, string(v.Status), string(v.Status), v.StepResult.Duration); err != nil {
			session.Logger.Warn("record episodic failed",
				zap.String("target", target), zap.Error(err))
		}
	}
	return nil
}

// applyEffectiveness updates procedural memory effectiveness using EMA
// over memory_usage rows that haven't been consolidated yet.
// Shared by both runPhase and resumePhase consolidate logic.
func applyEffectiveness(ctx context.Context, session *Session, verdicts []examiner.FinalVerdict) error {
	rows, err := session.Store.UnconsolidatedUsage(ctx, session.ID)
	if err != nil {
		return err
	}
	// Group by procedural_id; gather each group's case verdicts (skip excluded).
	verdictByTarget := verdictByNormalizedTarget(ctx, session, verdicts)

	type group struct {
		procID               int64
		passes, fails, count int
		ids                  []int64
	}
	groups := map[int64]*group{}
	var order []int64
	for _, u := range rows {
		g, ok := groups[u.ProceduralID]
		if !ok {
			g = &group{procID: u.ProceduralID}
			groups[u.ProceduralID] = g
			order = append(order, u.ProceduralID)
		}
		g.ids = append(g.ids, u.ID)
		st, found := verdictByTarget[memory.NormalizeTarget(u.Target)]
		if !found {
			continue // verdict not committed/in-memory for this target
		}
		switch st {
		case examiner.StatusPass:
			g.passes++
			g.count++
		case examiner.StatusFail:
			g.fails++
			g.count++
		default: // skip/uncertain excluded
		}
	}
	for _, pid := range order {
		g := groups[pid]
		if g.count == 0 {
			// All-skip: no signal, but mark consolidated so we don't reprocess.
			if err := session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
				session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
			}
			continue
		}
		signal := float64(g.passes) / float64(g.count)
		if err := session.Store.ApplyProceduralEMA(ctx, pid, signal, g.count); err != nil {
			session.Logger.Warn("apply EMA failed", zap.Int64("proc", pid), zap.Error(err))
		}
		if err := session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
			session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
		}
	}
	return nil
}

// verdictByNormalizedTarget maps normalized target -> verdict status, drawing
// from committed verdicts (GetVerdicts) so resume sees only what persisted.
// Shared by both runPhase and resumePhase consolidate logic.
func verdictByNormalizedTarget(ctx context.Context, session *Session, verdicts []examiner.FinalVerdict) map[string]examiner.JudgeStatus {
	out := map[string]examiner.JudgeStatus{}
	committed, err := session.Store.GetVerdicts(ctx, session.ID)
	if err != nil {
		session.Logger.Warn("get verdicts failed", zap.Error(err))
	}
	add := func(target, status string) {
		if target == "" {
			return
		}
		out[memory.NormalizeTarget(target)] = examiner.JudgeStatus(status)
	}
	for _, v := range committed {
		add(v.Target, v.Status)
	}
	// In-memory covers skip cases that were never committed (TraceID==0).
	for _, v := range verdicts {
		if v.StepResult.TestCase != nil {
			add(v.StepResult.TestCase.Target, string(v.Status))
		}
	}
	return out
}

// archiveStale runs governance archival policies on stale memories.
// Shared by both runPhase and resumePhase consolidate logic.
func archiveStale(ctx context.Context, session *Session) error {
	store := session.Store
	project := session.Config.Project.Name

	if n, err := store.AutoArchiveLowEffectiveness(ctx, project); err != nil {
		session.Logger.Warn("archive procedural failed", zap.Error(err))
	} else if n > 0 {
		session.Logger.Info("archived stale procedural memory", zap.Int("count", n))
	}

	if n, err := store.ArchiveStaleEpisodic(ctx, 30); err != nil {
		session.Logger.Warn("archive episodic failed", zap.Error(err))
	} else if n > 0 {
		session.Logger.Info("archived stale episodic memory", zap.Int("count", n))
	}

	if n, err := store.ArchiveStaleSemantic(ctx, 90); err != nil {
		session.Logger.Warn("archive semantic failed", zap.Error(err))
	} else if n > 0 {
		session.Logger.Info("archived stale semantic memory", zap.Int("count", n))
	}

	return nil
}

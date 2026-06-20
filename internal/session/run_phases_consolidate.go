package session

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
)

// executeConsolidatePhase runs after verdicts are committed. It is idempotent
// (safe on resume): episodic writes key on session+target+verdict, effectiveness
// EMA is guarded by memory_usage.consolidated_at, and Learn dedups via upsert.
func (rp *runPhase) executeConsolidatePhase() error {
	if err := rp.writeEpisodicMemory(); err != nil {
		rp.session.Logger.Warn("episodic consolidate failed", zap.Error(err))
	}
	if err := rp.applyEffectiveness(); err != nil {
		rp.session.Logger.Warn("effectiveness consolidate failed", zap.Error(err))
	}
	if err := rp.archiveStale(); err != nil {
		rp.session.Logger.Warn("archive stale failed", zap.Error(err))
	}
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

func (rp *runPhase) applyEffectiveness() error {
	ctx := rp.ctx
	rows, err := rp.session.Store.UnconsolidatedUsage(ctx, rp.session.ID)
	if err != nil {
		return err
	}
	// Group by procedural_id; gather each group's case verdicts (skip excluded).
	verdictByTarget := rp.verdictByNormalizedTarget()

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
			if err := rp.session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
				rp.session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
			}
			continue
		}
		signal := float64(g.passes) / float64(g.count)
		if err := rp.session.Store.ApplyProceduralEMA(ctx, pid, signal, g.count); err != nil {
			rp.session.Logger.Warn("apply EMA failed", zap.Int64("proc", pid), zap.Error(err))
		}
		if err := rp.session.Store.MarkUsageConsolidated(ctx, g.ids); err != nil {
			rp.session.Logger.Warn("mark usage consolidated failed", zap.Error(err))
		}
	}
	return nil
}

// verdictByNormalizedTarget maps normalized target -> verdict status, drawing
// from committed verdicts (GetVerdicts) so resume sees only what persisted.
func (rp *runPhase) verdictByNormalizedTarget() map[string]examiner.JudgeStatus {
	out := map[string]examiner.JudgeStatus{}
	committed, err := rp.session.Store.GetVerdicts(rp.ctx, rp.session.ID)
	if err != nil {
		rp.session.Logger.Warn("get verdicts failed", zap.Error(err))
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
	for _, v := range rp.verdicts {
		if v.StepResult.TestCase != nil {
			add(v.StepResult.TestCase.Target, string(v.Status))
		}
	}
	return out
}

// archiveStale runs governance archival policies on stale memories.
func (rp *runPhase) archiveStale() error {
	store := rp.session.Store
	project := rp.session.Config.Project.Name

	if n, err := store.AutoArchiveLowEffectiveness(rp.ctx, project); err != nil {
		rp.session.Logger.Warn("archive procedural failed", zap.Error(err))
	} else if n > 0 {
		rp.session.Logger.Info("archived stale procedural memory", zap.Int("count", n))
	}

	if n, err := store.ArchiveStaleEpisodic(rp.ctx, 30); err != nil {
		rp.session.Logger.Warn("archive episodic failed", zap.Error(err))
	} else if n > 0 {
		rp.session.Logger.Info("archived stale episodic memory", zap.Int("count", n))
	}

	if n, err := store.ArchiveStaleSemantic(rp.ctx, 90); err != nil {
		rp.session.Logger.Warn("archive semantic failed", zap.Error(err))
	} else if n > 0 {
		rp.session.Logger.Info("archived stale semantic memory", zap.Int("count", n))
	}

	return nil
}

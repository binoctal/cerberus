package session

import (
	"fmt"

	"go.uber.org/zap"

	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// executeRemainingCases runs the agent execution phase for remaining cases
func (rp *resumePhase) executeRemainingCases() error {
	if rp.plan == nil {
		return fmt.Errorf("no plan to execute")
	}

	projectDir := rp.session.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}

	engine := agent.NewRuleEngine(rp.session.Config.Services, rp.session.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, agent.ServiceHeadersMap(rp.session.Config.Services), rp.session.Gate, rp.session.Logger)
	config := agent.DefaultReActConfig()
	emb := embedPkg.NewTrigramProvider(embedPkg.DefaultDimension)
	loop := agent.NewReActLoopWithGateWithConfig(agent.ReActLoopConfig{
		Driver:   rp.session.driverFor(&rp.session.agentDriver),
		Store:    rp.session.Store,
		Engine:   engine,
		Executor: multiExec,
		Config:   config,
		Gate:     rp.session.Gate,
		Logger:   rp.session.Logger,
		Embedder: emb,
		Project:  rp.session.Config.Project.Name,
	})

	var err error
	if rp.session.Parallel {
		workers := rp.session.MaxWorkers
		if workers <= 0 {
			workers = 4
		}
		pExec := agent.NewParallelExecutor(loop, agent.ParallelConfig{MaxWorkers: workers}, rp.session.Logger)
		rp.results, err = pExec.ExecutePlan(rp.ctx, rp.plan, rp.session.ID)
	} else {
		rp.results, err = loop.ExecutePlan(rp.ctx, rp.plan, rp.session.ID)
	}

	return err
}

// examineResults runs the examiner phase on the results, then assesses coverage
// when a contract is present (mirroring run_phases_examiner.go).
func (rp *resumePhase) examineResults() error {
	examinerCfg := examiner.DefaultExaminerConfig()
	if rp.session.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = rp.session.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = rp.session.Config.Settings.AutoFix
	}
	examinerCfg.MaxWorkers = rp.session.MaxWorkers

	examinerHead := examiner.NewExaminer(rp.session.driverFor(&rp.session.examinerDriver), rp.session.criticDriver, rp.session.Store, examinerCfg, rp.session.Logger)

	var err error
	rp.verdicts, rp.reflections, err = examinerHead.Examine(rp.ctx, rp.results, rp.session.ID, rp.session.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("examiner (resume): %w", err)
	}

	rp.session.Logger.Info("examination complete (resume)",
		zap.Int("verdicts", len(rp.verdicts)),
		zap.Int("reflections_stored", rp.reflections),
	)

	// Assess coverage against contract if present (shared with run path).
	assessCoverageIfContract(rp.ctx, rp.session, examinerHead, rp.results)

	return nil
}

// executeConsolidatePhase runs after verdicts are committed during resume.
// It is idempotent (safe on resume): episodic writes key on session+target+verdict,
// effectiveness EMA is guarded by memory_usage.consolidated_at, and Learn dedups via upsert.
// This mirrors the runPhase consolidate logic exactly, ensuring resumed sessions
// get full effectiveness tracking and governance.
func (rp *resumePhase) executeConsolidatePhase() error {
	if err := writeEpisodicMemory(rp.ctx, rp.session, rp.verdicts); err != nil {
		rp.session.Logger.Warn("episodic consolidate failed (resume)", zap.Error(err))
	}
	if err := applyEffectiveness(rp.ctx, rp.session, rp.verdicts); err != nil {
		rp.session.Logger.Warn("effectiveness consolidate failed (resume)", zap.Error(err))
	}
	if err := archiveStale(rp.ctx, rp.session); err != nil {
		rp.session.Logger.Warn("archive stale failed (resume)", zap.Error(err))
	}
	return nil
}

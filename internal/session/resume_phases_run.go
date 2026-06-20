package session

import (
	"fmt"

	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/memory"
	"go.uber.org/zap"
)

// executeRemainingCases runs the agent execution phase for remaining cases
func (rp *resumePhase) executeRemainingCases() error {
	if rp.plan == nil {
		return fmt.Errorf("no plan to execute")
	}

	baseURL := rp.session.resolveBaseURL()
	projectDir := rp.session.ProjectDir
	if projectDir == "" {
		projectDir = "."
	}

	engine := agent.NewRuleEngine(baseURL, rp.session.Config.Actors, projectDir)
	multiExec := agent.BuildMultiExecutor(projectDir, rp.session.Gate, rp.session.Logger)
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

// examineResults runs the examiner phase on the results
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

	return nil
}

// executeConsolidatePhase runs after verdicts are committed during resume.
// It is idempotent (safe on resume): episodic writes key on session+target+verdict.
func (rp *resumePhase) executeConsolidatePhase() error {
	for _, v := range rp.verdicts {
		tc := v.StepResult.TestCase
		if tc == nil || tc.Target == "" {
			continue
		}
		target := memory.NormalizeTarget(tc.Target)
		if err := rp.session.Store.RecordEpisodic(
			rp.ctx, rp.session.ID, target, string(v.Status), string(v.Status), v.StepResult.Duration); err != nil {
			rp.session.Logger.Warn("record episodic failed (resume)",
				zap.String("target", target), zap.Error(err))
		}
	}
	return nil
}

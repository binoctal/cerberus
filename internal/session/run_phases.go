package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/config"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// runPhase holds state for a single run phase
type runPhase struct {
	session    *Session
	ctx        context.Context
	startTime  time.Time
	plan       *agent.TestPlan
	results    []agent.StepResult
	verdicts   []examiner.FinalVerdict
	reflections int
	summary    *SessionSummary
	err        error
}

// initialize prepares the session for running
func (rp *runPhase) initialize() error {
	rp.session.Logger.Info("session starting", zap.String("id", rp.session.ID))
	return nil
}

// finalize updates session stats and status after completion
func (rp *runPhase) finalize() {
	elapsed := time.Since(rp.startTime)
	tokensUsed := rp.session.Driver.Budget().SessionTotal - rp.session.Driver.Budget().Remaining()

	// Build summary if not yet built (e.g. on early error).
	if rp.summary == nil {
		rp.summary = &SessionSummary{
			Goal:        rp.session.Goal,
			TotalTokens: tokensUsed,
			Duration:    elapsed.Round(time.Millisecond).String(),
			DurationMs:  elapsed.Milliseconds(),
		}
	} else {
		rp.summary.TotalTokens = tokensUsed
		rp.summary.Duration = elapsed.Round(time.Millisecond).String()
		rp.summary.DurationMs = elapsed.Milliseconds()
	}

	// Write stats to store.
	if statsErr := rp.session.Store.UpdateSessionStats(rp.ctx, rp.session.ID, rp.summary.CoveragePct, rp.summary); statsErr != nil {
		rp.session.Logger.Error("update session stats", zap.Error(statsErr))
	}

	// Print human-readable summary.
	rp.session.Logger.Info("session summary", zap.String("summary", rp.summary.String()))

	// Update status (terminal).
	status := "completed"
	if rp.err != nil {
		status = "failed"
	}
	if updateErr := rp.session.Store.UpdateSessionStatus(rp.ctx, rp.session.ID, status); updateErr != nil {
		rp.session.Logger.Error("update session status", zap.Error(updateErr))
	}
}

// executeScoutPhase runs the Scout analysis and planning phase
func (rp *runPhase) executeScoutPhase() (*project.ProjectModel, error) {
	scoutHead := scout.NewScout(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Store, rp.session.Config, rp.session.Logger)

	// Scale ToT/Reflexion depth to the Scout model's context window
	scoutModel := rp.session.tiers[config.HeadScout]
	scoutCtx := 0
	if scoutModel != "" {
		scoutCtx = llm.ContextWindow(scoutModel)
	}
	scoutHead.SetReflexion(config.ResolveReflexionConfig(rp.session.Config.Settings, scoutCtx))

	if rp.session.DeepPlan {
		scoutHead.SetDeepPlan(
			config.ResolveToTConfig(rp.session.Config.Settings, scoutCtx),
			rp.session.driverFor(&rp.session.scoutDriver),
			rp.session.driverFor(&rp.session.agentDriver),
		)
	}

	model, err := scoutHead.Analyze(rp.ctx, scout.TargetInfo{
		URL:  rp.session.resolveBaseURL(),
		Goal: rp.session.Goal,
	})
	if err != nil {
		return nil, fmt.Errorf("scout analyze: %w", err)
	}

	plan, err := scoutHead.Plan(rp.ctx, rp.session.Goal, model)
	if err != nil {
		return nil, fmt.Errorf("scout plan: %w", err)
	}
	rp.plan = plan

	// Persist plan for potential resumption.
	if saveErr := rp.session.Store.SavePlan(rp.ctx, rp.session.ID, plan); saveErr != nil {
		rp.session.Logger.Warn("failed to save plan", zap.Error(saveErr))
	}

	return model, nil
}

// executeAgentPhase runs the Agent execution phase
func (rp *runPhase) executeAgentPhase() error {
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
	loop := agent.NewReActLoopWithGate(rp.session.driverFor(&rp.session.agentDriver), rp.session.Store, engine, multiExec, config, rp.session.Gate, rp.session.Logger)

	rp.session.Logger.Info("executing test plan",
		zap.String("session_id", rp.session.ID),
		zap.Int("cases", len(rp.plan.Cases)),
		zap.Bool("parallel", rp.session.Parallel),
	)

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

// executeExaminerPhase runs the Examiner judgment and learning phase
func (rp *runPhase) executeExaminerPhase() error {
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
		return fmt.Errorf("examiner: %w", err)
	}

	rp.session.Logger.Info("examination complete",
		zap.Int("verdicts", len(rp.verdicts)),
		zap.Int("reflections_stored", rp.reflections),
	)

	return nil
}

// executeAutoTestPhase runs the optional AutoTest coverage-driven test generation
func (rp *runPhase) executeAutoTestPhase() {
	if rp.session.AutoTestSafety == "" || rp.session.AutoTestSafety == "off" {
		return
	}

	mode := autotest.SafetyMode(rp.session.AutoTestSafety)
	cov := autotest.NewGoCoverageProvider(autotest.DefaultGoCoverageRunner, rp.session.Logger)
	gen := autotest.NewGoTestGenerator(rp.session.driverFor(&rp.session.scoutDriver), rp.session.Logger)
	at := autotest.NewAutoTest(cov, gen, autotest.NewEscalationGateAdapter(rp.session.Gate), nil, mode, rp.session.Logger)

	report, atErr := at.Run(rp.ctx, rp.session.ProjectDir)
	if atErr != nil {
		rp.session.Logger.Warn("autotest phase failed", zap.Error(atErr))
		return
	}

	if report != nil {
		rp.session.Logger.Info("autotest phase complete",
			zap.String("mode", string(mode)),
			zap.Int("gaps", len(report.Gaps)),
			zap.Int("generated", len(report.Generated)),
			zap.Int("written", len(report.Written)),
			zap.Int("reverted", len(report.Reverted)),
			zap.Float64("before_pct", report.BeforeCoveragePct),
			zap.Float64("after_pct", report.AfterCoveragePct))

		// dry-run: print each generated test for review
		if mode == autotest.SafetyDryRun {
			fmt.Println("\nAutoTest dry-run — generated test previews:")
			for _, tf := range report.Generated {
				fmt.Printf("\n--- %s ---\n%s\n", tf.Path, tf.Content)
			}
		}

		rp.session.LastAutoTestReport = report

		// Persist AutoTest report to DB (best-effort, non-blocking).
		if perr := rp.session.Store.UpdateSessionAutoTest(rp.ctx, rp.session.ID, report); perr != nil {
			rp.session.Logger.Warn("persist autotest report", zap.Error(perr))
		}
	}
}

// buildSummary constructs the session summary
func (rp *runPhase) buildSummary(model *project.ProjectModel) {
	if rp.plan == nil {
		return
	}

	rp.summary = FromResults(
		rp.session.Goal,
		rp.session.resolveBaseURL(),
		len(rp.plan.Cases),
		rp.results,
		rp.verdicts,
		rp.reflections,
		0, // tokens filled in finalize
		time.Since(rp.startTime),
	)

	if model != nil {
		rp.summary.EndpointsFound = len(model.API.Endpoints)
	}
}

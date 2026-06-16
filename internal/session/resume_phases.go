package session

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// resumePhase holds state for resume operations
type resumePhase struct {
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

// initialize prepares the session for resuming
func (rp *resumePhase) initialize() error {
	rp.session.Logger.Info("resuming session", zap.String("id", rp.session.ID))
	return nil
}

// finalize updates session stats and status after completion
func (rp *resumePhase) finalize() {
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

	rp.session.Logger.Info("session summary", zap.String("summary", rp.summary.String()))

	// Update status (terminal).
	status := "completed"
	if rp.err != nil {
		status = "failed"
	}
	if statsErr := rp.session.Store.UpdateSessionStatus(rp.ctx, rp.session.ID, status); statsErr != nil {
		rp.session.Logger.Error("update session status", zap.Error(statsErr))
	}
}

// loadSavedPlan loads the test plan from the database
func (rp *resumePhase) loadSavedPlan() error {
	var plan agent.TestPlan
	if err := rp.session.Store.LoadPlan(rp.ctx, rp.session.ID, &plan); err != nil {
		return fmt.Errorf("load plan for session %s: %w", rp.session.ID, err)
	}

	if len(plan.Cases) == 0 {
		return fmt.Errorf("saved plan has no test cases")
	}

	rp.plan = &plan
	return nil
}

// filterRemainingCases filters out completed test cases from the plan
func (rp *resumePhase) filterRemainingCases() error {
	// Get completed targets.
	completed, err := rp.session.Store.GetCompletedTargets(rp.ctx, rp.session.ID)
	if err != nil {
		return fmt.Errorf("get completed targets: %w", err)
	}

	// Filter out completed cases.
	var remaining []agent.TestCase
	for _, tc := range rp.plan.Cases {
		if !completed[tc.Target] {
			remaining = append(remaining, tc)
		}
	}

	rp.session.Logger.Info("resuming from saved plan",
		zap.Int("total_cases", len(rp.plan.Cases)),
		zap.Int("completed", len(completed)),
		zap.Int("remaining", len(remaining)),
	)

	if len(remaining) == 0 {
		rp.session.Logger.Info("all cases already completed")
		// Build minimal summary for already completed session
		rp.summary = &SessionSummary{
			Goal:        rp.session.Goal,
			Duration:    time.Since(rp.startTime).String(),
			DurationMs:  time.Since(rp.startTime).Milliseconds(),
		}
		return fmt.Errorf("all cases already completed")
	}

	// Build a reduced plan with only remaining cases.
	resumePlan := &agent.TestPlan{
		Goal:       rp.plan.Goal,
		Cases:      remaining,
		ProjectURL: rp.plan.ProjectURL,
	}
	rp.plan = resumePlan

	return nil
}

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
	loop := agent.NewReActLoopWithGate(rp.session.driverFor(&rp.session.agentDriver), rp.session.Store, engine, multiExec, config, rp.session.Gate, rp.session.Logger)

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

// buildSummary constructs the session summary for resumed portion
func (rp *resumePhase) buildSummary() {
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
}

package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// executeExaminerPhase runs the Examiner judgment and learning phase
func (rp *runPhase) executeExaminerPhase() error {
	fmt.Printf("• Examiner: judging %d results...\n", len(rp.results))
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

	// Assess coverage against contract if present
	if rp.session.Contract != nil {
		// Compute coverage from results: passed / total * 100
		// covPct is a test-case pass-ratio proxy for line coverage in v1.
		// TODO(Plan2): wire real line coverage from AutoTest report's Before/AfterCoveragePct.
		covPct := 0.0
		if len(rp.results) > 0 {
			passed := 0
			for _, r := range rp.results {
				if r.Status == agent.StepPassed {
					passed++
				}
			}
			covPct = float64(passed) / float64(len(rp.results)) * 100
		}
		assessment, aerr := examinerHead.AssessCoverage(rp.ctx, rp.session.Contract, rp.results, covPct)
		if aerr == nil {
			rp.session.Assessment = assessment
			rp.session.Logger.Info("coverage assessment",
				zap.Bool("reached", assessment.Reached),
				zap.Int("gaps", len(assessment.Gaps)),
				zap.Float64("coverage_pct", assessment.CoveragePct))
		} else {
			rp.session.Logger.Warn("coverage assessment failed", zap.Error(aerr))
		}
	}

	return nil
}

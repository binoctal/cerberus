package session

import (
	"fmt"

	"go.uber.org/zap"

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
		// Use real line coverage from AutoTest report or independent coverage run.
		// rp.session.lineCoverage honors an injected stub (tests) to avoid
		// recursively running go test/jest/pytest when ProjectDir is a module
		// under test.
		covPct := rp.session.lineCoverage(rp.ctx)
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

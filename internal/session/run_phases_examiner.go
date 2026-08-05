package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/project"
)

// buildExaminer constructs the Examiner head from session config. Shared by
// executeExaminerPhase and the repair loop (re-judge of replacements).
func (rp *runPhase) buildExaminer() *examiner.Examiner {
	examinerCfg := examiner.DefaultExaminerConfig()
	if rp.session.Config.Settings.ConfidenceThreshold > 0 {
		examinerCfg.ConfThreshold = rp.session.Config.Settings.ConfidenceThreshold
		examinerCfg.AutoFix = rp.session.Config.Settings.AutoFix
	}
	examinerCfg.MaxWorkers = rp.session.MaxWorkers
	examinerCfg.VocabSummary = project.RenderVocabSummary(rp.session.Config.Services)
	return examiner.NewExaminer(rp.session.driverFor(&rp.session.examinerDriver), rp.session.criticDriver, rp.session.Store, examinerCfg, rp.session.Logger)
}

// executeExaminerPhase runs the Examiner judgment and learning phase
func (rp *runPhase) executeExaminerPhase() error {
	fmt.Printf("• Examiner: judging %d results...\n", len(rp.results))
	examinerHead := rp.buildExaminer()

	var err error
	rp.verdicts, rp.reflections, err = examinerHead.Examine(rp.ctx, rp.results, rp.session.ID, rp.session.Config.Project.Name)
	if err != nil {
		return fmt.Errorf("examiner: %w", err)
	}

	rp.session.Logger.Info("examination complete",
		zap.Int("verdicts", len(rp.verdicts)),
		zap.Int("reflections_stored", rp.reflections),
	)

	// Assess coverage against contract if present (shared with resume path).
	assessCoverageIfContract(rp.ctx, rp.session, examinerHead, rp.results)

	return nil
}

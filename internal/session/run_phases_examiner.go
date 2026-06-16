package session

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/examiner"
)

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

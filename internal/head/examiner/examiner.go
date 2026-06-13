package examiner

import (
	"context"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
	"go.uber.org/zap"
)

// Examiner is the third Cerberus head: Judge (Self-Refine) + Learn (Reflexion).
// It evaluates test results and generates learning for future sessions.
type Examiner struct {
	judge   *Judge
	learner *Learner
	store   *store.Store
	logger    *zap.Logger
	config    ExaminerConfig
	autoFixer *AutoFixer
}

// NewExaminer creates an Examiner head.
// criticDriver can be nil to disable Self-Refine critique.
func NewExaminer(judgeDriver, criticDriver *ai.Driver, s *store.Store, config ExaminerConfig, logger *zap.Logger) *Examiner {
	return &Examiner{
		judge:   NewJudge(judgeDriver, criticDriver, config),
		learner: NewLearner(judgeDriver, s, logger),
		store:   s,
		logger:  logger,
		config:    config,
		autoFixer: NewAutoFixer(judgeDriver, logger),
	}
}

// Examine evaluates all step results through Judge + Policy, then triggers Reflexion learning.
// Returns final verdicts and count of stored reflections.
func (e *Examiner) Examine(ctx context.Context, results []agent.StepResult, sessionID, projectName string) ([]FinalVerdict, int, error) {
	verdicts := make([]FinalVerdict, 0, len(results))

	for _, r := range results {
		// Judge: Self-Refine evaluation.
		judgeResult, err := e.judge.Judge(ctx, r)
		if err != nil {
			e.logger.Warn("judge failed, using step status as verdict",
				zap.String("case_id", r.TestCase.ID),
				zap.Error(err),
			)
			// Fallback: use the step result status directly.
			judgeResult = &JudgeResult{
				Status:                stepStatusToJudgeStatus(r.Status),
				ExistenceConfidence:   1.0,
				CorrectnessConfidence: 1.0,
				Reasoning:             "Judge failed, using execution status",
			}
		}

		// Policy: Uncertain degradation chain.
		verdict := VerdictPolicy(judgeResult, r, e.config.ConfThreshold)
		verdicts = append(verdicts, verdict)

		e.logger.Info("verdict",
			zap.String("case_id", r.TestCase.ID),
			zap.String("status", string(verdict.Status)),
			zap.Float64("correctness", verdict.CorrectnessConfidence),
			zap.Int("degraded_level", verdict.DegradedLevel),
			zap.Bool("critique", verdict.CritiqueTriggered),
		)
	}

	// Reflexion: learn from all results.
	reflectionsStored, err := e.learner.Learn(ctx, LearnInput{
		SessionID: sessionID,
		Project:   projectName,
		Results:   results,
	})
	if err != nil {
		e.logger.Warn("reflexion learning failed", zap.Error(err))
		// Non-fatal: verdicts are still valid.
	}

	return verdicts, reflectionsStored, nil
}

// stepStatusToJudgeStatus converts an agent step status to a judge status.
func stepStatusToJudgeStatus(s agent.StepStatus) JudgeStatus {
	switch s {
	case agent.StepPassed:
		return StatusPass
	case agent.StepFailed:
		return StatusFail
	case agent.StepSkipped:
		return StatusSkip
	default:
		return StatusUncertain
	}
}

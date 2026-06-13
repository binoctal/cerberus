package examiner

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/store"
)

// Examiner is the third Cerberus head: Judge (Self-Refine) + Learn (Reflexion).
// It evaluates test results and generates learning for future sessions.
type Examiner struct {
	judge     *Judge
	learner   *Learner
	store     *store.Store
	logger    *zap.Logger
	config    ExaminerConfig
	autoFixer *AutoFixer
}

// NewExaminer creates an Examiner head.
// criticDriver can be nil to disable Self-Refine critique.
func NewExaminer(judgeDriver, criticDriver *ai.Driver, s *store.Store, config ExaminerConfig, logger *zap.Logger) *Examiner {
	return &Examiner{
		judge:     NewJudge(judgeDriver, criticDriver, config),
		learner:   NewLearner(judgeDriver, s, logger, nil),
		store:     s,
		logger:    logger,
		config:    config,
		autoFixer: NewAutoFixer(judgeDriver, logger),
	}
}

// Examine evaluates all step results through Judge + Policy in parallel, then
// triggers Reflexion learning once over the full result set.
//
// Judge calls are independent per case (no cross-case state until aggregation),
// so they run concurrently bounded by ExaminerConfig.MaxWorkers. Verdicts are
// written by index into a pre-allocated slice, so the returned order matches the
// input order exactly — parallelization is transparent to callers. Reflexion
// Learn runs once after all verdicts (it is an aggregation step and must see
// every result).
// Returns final verdicts and count of stored reflections.
func (e *Examiner) Examine(ctx context.Context, results []agent.StepResult, sessionID, projectName string) ([]FinalVerdict, int, error) {
	verdicts := make([]FinalVerdict, len(results))

	workers := e.config.MaxWorkers
	if workers <= 0 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup

	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			r := results[idx]

			// Acquire a worker slot, or bail to a fallback verdict on cancellation.
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				verdicts[idx] = fallbackVerdict(r, e.config.ConfThreshold, "context canceled, using execution status")
				return
			}
			defer func() { <-sem }()

			// Judge: Self-Refine evaluation.
			judgeResult, err := e.judge.Judge(ctx, r)
			if err != nil {
				e.logger.Warn("judge failed, using step status as verdict",
					zap.String("case_id", r.TestCase.ID),
					zap.Error(err),
				)
				verdicts[idx] = fallbackVerdict(r, e.config.ConfThreshold, "Judge failed, using execution status")
				e.logger.Info("verdict",
					zap.String("case_id", r.TestCase.ID),
					zap.String("status", string(verdicts[idx].Status)),
					zap.Int("degraded_level", verdicts[idx].DegradedLevel),
				)
				return
			}

			// Policy: Uncertain degradation chain.
			verdict := VerdictPolicy(judgeResult, r, e.config.ConfThreshold)
			verdicts[idx] = verdict

			e.logger.Info("verdict",
				zap.String("case_id", r.TestCase.ID),
				zap.String("status", string(verdict.Status)),
				zap.Float64("correctness", verdict.CorrectnessConfidence),
				zap.Int("degraded_level", verdict.DegradedLevel),
				zap.Bool("critique", verdict.CritiqueTriggered),
			)
		}(i)
	}
	wg.Wait()

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

// fallbackVerdict builds a verdict from a step's own status when Judge cannot
// run (LLM failure or context cancellation). Confidence is maximal so Policy
// treats it as a deterministic status rather than uncertain.
func fallbackVerdict(r agent.StepResult, conf float64, reason string) FinalVerdict {
	judgeResult := &JudgeResult{
		Status:                stepStatusToJudgeStatus(r.Status),
		ExistenceConfidence:   1.0,
		CorrectnessConfidence: 1.0,
		Reasoning:             reason,
	}
	return VerdictPolicy(judgeResult, r, conf)
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

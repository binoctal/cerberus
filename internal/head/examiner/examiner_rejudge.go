package examiner

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// rejudgeRateLimited re-judges the verdicts that fell back to execution status
// because of provider quota exhaustion (llm.RateLimitError). Provider windows
// reset on their own schedule (anthropic 5h window, code 1308), so the pass
// waits until the latest advertised reset — bounded by
// ExaminerConfig.RateLimitRewaitMax — and retries each affected case once.
// Verdicts whose retry fails again keep their fallback; the pass never
// degrades a verdict, only upgrades it.
func (e *Examiner) rejudgeRateLimited(ctx context.Context, results []agent.StepResult, verdicts []FinalVerdict, rateLimited []bool, reset time.Time) {
	var idxs []int
	for i, rl := range rateLimited {
		if rl {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) == 0 {
		return
	}

	wait := time.Until(reset)
	maxWait := e.config.RateLimitRewaitMax
	if maxWait <= 0 {
		maxWait = 30 * time.Minute
	}
	if reset.IsZero() || wait > maxWait {
		e.logger.Warn("rate-limited verdicts keep step-status fallback",
			zap.Int("count", len(idxs)),
			zap.Time("reset", reset),
			zap.Duration("would_wait", wait),
			zap.Duration("max_wait", maxWait),
		)
		return
	}
	if wait > 0 {
		e.logger.Info("provider quota window pending — waiting before re-judging",
			zap.Int("fallback_verdicts", len(idxs)),
			zap.Time("reset", reset),
			zap.Duration("wait", wait),
		)
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return
		}
	}

	workers := e.config.MaxWorkers
	if workers <= 0 {
		workers = 4
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for _, idx := range idxs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()

			judgeResult, err := e.judge.Judge(ctx, results[i])
			if err != nil {
				e.logger.Warn("re-judge failed, keeping step-status fallback",
					zap.String("case_id", results[i].TestCase.ID),
					zap.Error(err),
				)
				return
			}
			verdicts[i] = VerdictPolicy(judgeResult, results[i], e.config.ConfThreshold)
			e.logger.Info("verdict re-judged",
				zap.String("case_id", results[i].TestCase.ID),
				zap.String("status", string(verdicts[i].Status)),
				zap.Int("degraded_level", verdicts[i].DegradedLevel),
			)
		}(idx)
	}
	wg.Wait()
}

package agent

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"
)

// checkTokenBudgetExhaustion checks if steer failed due to token budget
func checkTokenBudgetExhaustion(err error) bool {
	return err != nil && err.Error() == "token budget exhausted"
}

// buildSkippedResultForTokenBudget creates a skipped result for token budget exhaustion
func buildSkippedResultForTokenBudget(tc *TestCase, traceID int64, attempt int, start time.Time, err error) StepResult {
	return StepResult{
		TestCase: tc,
		Status:   StepSkipped,
		TraceID:  traceID,
		Attempts: attempt,
		Duration: time.Since(start),
		Error:    err,
	}
}

// buildSkippedResultForDestructiveAction creates a skipped result for destructive actions
func buildSkippedResultForDestructiveAction(tc *TestCase, traceID int64, attempt int, start time.Time, action types.TypedAction) StepResult {
	return StepResult{
		TestCase: tc,
		Status:   StepSkipped,
		TraceID:  traceID,
		Attempts: attempt,
		Duration: time.Since(start),
		Error:    fmt.Errorf("skipped destructive action: %s %s", action.GetActionType(), action.Target()),
	}
}

// buildFailedResultForUnreachableTarget creates a failed result for unreachable targets
func buildFailedResultForUnreachableTarget(tc *TestCase, traceID int64, attempt int, start time.Time, target string) StepResult {
	return StepResult{
		TestCase: tc,
		Status:   StepFailed,
		TraceID:  traceID,
		Attempts: attempt,
		Duration: time.Since(start),
		Error:    fmt.Errorf("target unreachable: %s", target),
	}
}

// buildPassedResult creates a successful result
func buildPassedResult(tc *TestCase, traceID int64, attempt int, start time.Time, action types.TypedAction, result types.ExecutorResult) StepResult {
	return StepResult{
		TestCase: tc,
		Status:   StepPassed,
		TraceID:  traceID,
		Attempts: attempt,
		Duration: time.Since(start),
		Action:   action,
		Result:   result,
		Evidence: []Evidence{{Type: evidenceType(result), Content: result.Evidence().Content}},
	}
}

// executeAndRecordAction executes the action and records evidence.
func executeAndRecordAction(r *ReActLoop, ctx context.Context, tc TestCase, action types.TypedAction, traceID int64) types.ExecutorResult {
	action = r.withServiceHeaders(tc, action)
	action = r.withActorHeaders(tc, action)
	action = r.withBaseURL(tc, action)
	result := r.executor.Execute(ctx, action)
	r.recordEvidence(ctx, traceID, "steer_attempt", action, result)
	return result
}

// withServiceHeaders merges the tc.Service's service-level headers (e.g. Host
// for domain routing) underneath the action's own headers, mirroring the rule
// engine's serviceHeaders. Without this the ReAct path reaches the target with
// Host derived from the URL host, which domain-routed gateways reject as an
// unknown domain. Final priority is service < actor < action.
func (r *ReActLoop) withServiceHeaders(tc TestCase, action types.TypedAction) types.TypedAction {
	if r.engine == nil {
		return action
	}
	ha, ok := action.(types.HTTPAction)
	if !ok {
		return action
	}
	svc := r.engine.serviceHeaders(tc)
	if len(svc) == 0 {
		return action
	}
	merged := make(map[string]string, len(svc)+len(ha.Headers))
	for k, v := range svc {
		merged[k] = v
	}
	for k, v := range ha.Headers {
		if v == "" {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	ha.Headers = merged
	return ha
}

// withBaseURL resolves a server-relative URL on HTTP and Navigate actions
// against tc.Service's URL via the engine's baseURLFor helper, mirroring what
// the rule engine already does (rules_http.go). LLM- and fallback-sourced
// actions frequently copy the test case's path-only target (e.g.
// "/v1/chat/completions"); without resolution the request cannot connect and
// the service-level headers — which are matched by URL host — never get injected.
// Actions with an absolute URL have their host:port forced to tc.Service's base,
// preserving the original path/query. Non-HTTP actions pass through unchanged.
func (r *ReActLoop) withBaseURL(tc TestCase, action types.TypedAction) types.TypedAction {
	base := ""
	if r.engine != nil {
		base = r.engine.baseURLFor(tc)
	}
	if base == "" {
		return action
	}
	switch a := action.(type) {
	case types.HTTPAction:
		a.URL = forceServiceHost(base, a.URL)
		return a
	case types.NavigateAction:
		a.URL = forceServiceHost(base, a.URL)
		return a
	}
	return action
}

// resolveActionURL returns target unchanged when it is already absolute;
// otherwise it joins the (relative) target onto base.
func resolveActionURL(base, target string) string {
	if target == "" || isAbsoluteURL(target) {
		return target
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(target, "/")
}

// forceServiceHost returns target unchanged if empty; otherwise rewrites the
// URL so its scheme+host+port come from base, while preserving the target's
// path and query. Relative targets are joined onto base. Absolute targets
// keep their path but take base's host:port (corrects wrong-port guesses).
func forceServiceHost(base, target string) string {
	if target == "" {
		return target
	}
	if !isAbsoluteURL(target) {
		return resolveActionURL(base, target) // relative → base + path (existing behavior)
	}
	baseURL, err := url.Parse(base)
	if err != nil {
		return target
	}
	tu, err := url.Parse(target)
	if err != nil {
		return target
	}
	tu.Scheme = baseURL.Scheme
	tu.Host = baseURL.Host
	return tu.String()
}

// isAbsoluteURL reports whether s has an http/https scheme.
func isAbsoluteURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// isNoopWait reports whether action is a duration-only wait with no selector or
// state — i.e. it merely delays and asserts nothing about the system under
// test. Such an action succeeding must not be judged as a passing step. A wait
// that targets a selector or state is a real UI probe, not a noop.
func isNoopWait(action types.TypedAction) bool {
	w, ok := action.(types.WaitAction)
	return ok && w.Selector == "" && w.WaitForState == ""
}

// withActorHeaders merges the active actor's Credentials.Headers underneath an
// HTTP action's own headers (action overrides; empty removes). Non-HTTP
// actions pass through unchanged. Combined with the executor's service-level
// TODO: per-service actor for ReAct path (currently uses actors[0]).
// headers, final priority is service < actor < action.
func (r *ReActLoop) withActorHeaders(tc TestCase, action types.TypedAction) types.TypedAction {
	if r.engine == nil || len(r.engine.actors) == 0 {
		return action
	}
	actor := r.engine.actors[0]
	if len(actor.Credentials.Headers) == 0 {
		return action
	}
	ha, ok := action.(types.HTTPAction)
	if !ok {
		return action
	}
	merged := make(map[string]string, len(actor.Credentials.Headers)+len(ha.Headers))
	for k, v := range actor.Credentials.Headers {
		merged[k] = v
	}
	for k, v := range ha.Headers {
		if v == "" {
			delete(merged, k)
			continue
		}
		merged[k] = v
	}
	ha.Headers = merged
	return ha
}

// updateConsecutiveTimeouts updates the consecutive timeout counter
func updateConsecutiveTimeouts(result types.ExecutorResult, currentCount int) int {
	if !result.Success() {
		return currentCount + 1
	}
	return 0
}

// logSteerAttempt logs the steer attempt details
func logSteerAttempt(logger *zap.Logger, attempt int, action types.TypedAction, result types.ExecutorResult) {
	logger.Info("steer attempt",
		zap.Int("attempt", attempt),
		zap.String("action_type", string(action.GetActionType())),
		zap.String("target", action.Target()),
		zap.Bool("success", result.Success()),
		zap.Duration("latency", result.Duration()),
	)
}

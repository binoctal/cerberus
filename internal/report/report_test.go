package report

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func setupTestStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	err = store.RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err)
	return s
}

func TestBuildReport(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Create a session.
	sess, err := s.CreateSession(ctx, "run", "test goal", "test-project")
	require.NoError(t, err)

	// Update stats.
	summary := session.SessionSummary{
		Goal:        "test goal",
		TotalCases:  5,
		Passed:      3,
		Failed:      1,
		Skipped:     1,
		Uncertain:   0,
		Duration:    "2.5s",
		DurationMs:  2500,
		TotalTokens: 10000,
	}
	err = s.UpdateSessionStats(ctx, sess.ID, 80.0, summary)
	require.NoError(t, err)
	err = s.UpdateSessionStatus(ctx, sess.ID, "completed")
	require.NoError(t, err)

	// Create a trace and verdict.
	traceID, err := s.CreateTrace(ctx, sess.ID, "http", "GET /api/health")
	require.NoError(t, err)
	err = s.FinishTrace(ctx, traceID, "pass")
	require.NoError(t, err)

	_, err = s.CreateVerdict(ctx, sess.ID, traceID, "GET /api/health", "pass", 0.95, "judge", "endpoint healthy", nil, store.FailureReasonNone)
	require.NoError(t, err)

	// Build report.
	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	assert.Equal(t, sess.ID, data.Session.ID)
	assert.Equal(t, "completed", data.Session.Status)
	assert.Equal(t, "test goal", data.Session.Goal)
	require.Len(t, data.Traces, 1)
	assert.Equal(t, "GET /api/health", data.Traces[0].Target)
	require.Len(t, data.Verdicts, 1)
	assert.Equal(t, "pass", data.Verdicts[0].Status)
	assert.Equal(t, 0.95, data.Verdicts[0].Confidence)
	assert.NotNil(t, data.Summary)
	assert.Equal(t, 5, data.Summary.TotalCases)
	assert.Equal(t, 3, data.Summary.Passed)
}

func TestBuildReport_NotFound(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	_, err := BuildReport(ctx, s, "nonexistent-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "session not found")
}

func TestBuildReport_EmptyTraces(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "empty test", "proj")
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)
	assert.Empty(t, data.Traces)
	assert.Empty(t, data.Verdicts)
}

func TestRenderMarkdown(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "markdown test", "proj")
	require.NoError(t, err)

	summary := session.SessionSummary{
		Goal:        "markdown test",
		TotalCases:  3,
		Passed:      2,
		Failed:      1,
		Duration:    "1s",
		DurationMs:  1000,
		TotalTokens: 5000,
	}
	_ = s.UpdateSessionStats(ctx, sess.ID, 66.0, summary)
	_ = s.UpdateSessionStatus(ctx, sess.ID, "completed")

	traceID, _ := s.CreateTrace(ctx, sess.ID, "http", "POST /api/login")
	_ = s.FinishTrace(ctx, traceID, "pass")
	_, _ = s.CreateVerdict(ctx, sess.ID, traceID, "POST /api/login", "pass", 0.9, "judge", "login works correctly", nil, store.FailureReasonNone)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	md := RenderMarkdown(data)
	assert.Contains(t, md, "# Cerberus Test Report")
	assert.Contains(t, md, "markdown test")
	assert.Contains(t, md, "POST /api/login")
	assert.Contains(t, md, "login works correctly")
	assert.Contains(t, md, "## Summary")
	assert.Contains(t, md, "## Verdicts")
	assert.Contains(t, md, "## Execution Timeline")
}

func TestRenderHTML(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "html test", "proj")
	require.NoError(t, err)

	summary := session.SessionSummary{
		Goal:       "html test",
		TotalCases: 2,
		Passed:     1,
		Failed:     1,
		Duration:   "500ms",
		DurationMs: 500,
	}
	_ = s.UpdateSessionStats(ctx, sess.ID, 50.0, summary)

	traceID, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /api/users")
	_ = s.FinishTrace(ctx, traceID, "pass")
	_, _ = s.CreateVerdict(ctx, sess.ID, traceID, "GET /api/users", "pass", 0.88, "judge", "users endpoint responds", nil, store.FailureReasonNone)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	html, err := RenderHTMLString(data)
	require.NoError(t, err)
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.Contains(t, html, "html test")
	assert.Contains(t, html, "GET /api/users")
	assert.Contains(t, html, "users endpoint responds")
	assert.Contains(t, html, "badge-pass")
	assert.Contains(t, html, "summary-card")
}

func TestRenderHTML_NoVerdicts(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "no verdicts", "proj")
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	html, err := RenderHTMLString(data)
	require.NoError(t, err)
	assert.Contains(t, html, "<!DOCTYPE html>")
	assert.NotContains(t, html, "Verdicts")
}

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"pass", "✅ pass"},
		{"fail", "❌ fail"},
		{"uncertain", "⚠️ uncertain"},
		{"skip", "⏭️ skip"},
		{"running", "🔄 running"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, statusEmoji(tt.input))
	}
}

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "500ms", FormatDuration(500))
	assert.Equal(t, "2.5s", FormatDuration(2500))
	assert.Equal(t, "1.0s", FormatDuration(1000))
}

func TestBuildReport_MultipleVerdicts(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "multi verdicts", "proj")
	require.NoError(t, err)

	trace1, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /a")
	trace2, _ := s.CreateTrace(ctx, sess.ID, "http", "POST /b")
	_ = s.FinishTrace(ctx, trace1, "pass")
	_ = s.FinishTrace(ctx, trace2, "fail")

	_, _ = s.CreateVerdict(ctx, sess.ID, trace1, "GET /a", "pass", 0.9, "judge", "ok", nil, store.FailureReasonNone)
	_, _ = s.CreateVerdict(ctx, sess.ID, trace2, "POST /b", "fail", 0.3, "judge", "server error", nil, store.FailureReasonAssertionFailed)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)
	require.Len(t, data.Verdicts, 2)

	md := RenderMarkdown(data)
	assert.True(t, strings.Contains(md, "GET /a"))
	assert.True(t, strings.Contains(md, "POST /b"))
	assert.True(t, strings.Contains(md, "server error"))
}

func TestBuildReport_EvidenceMap(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "evidence report", "proj")
	require.NoError(t, err)

	traceID, err := s.CreateTrace(ctx, sess.ID, "http", "GET /api/health")
	require.NoError(t, err)
	_ = s.FinishTrace(ctx, traceID, "pass")
	_, _ = s.CreateVerdict(ctx, sess.ID, traceID, "GET /api/health", "pass", 0.95, "judge", "healthy", nil, store.FailureReasonNone)

	// Add evidence.
	_, err = s.CreateEvidence(ctx, traceID, "screenshot", "base64-image-data")
	require.NoError(t, err)
	_, err = s.CreateEvidence(ctx, traceID, "response_body", `{"status":"ok"}`)
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	require.NotNil(t, data.Evidence)
	require.Len(t, data.Evidence[traceID], 2)
	assert.Equal(t, "screenshot", data.Evidence[traceID][0].Type)
	assert.Equal(t, "response_body", data.Evidence[traceID][1].Type)
}

func TestRenderMarkdown_WithEvidence(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "evidence md", "proj")
	require.NoError(t, err)
	_ = s.UpdateSessionStatus(ctx, sess.ID, "completed")

	traceID, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /api/items")
	_ = s.FinishTrace(ctx, traceID, "pass")
	_, _ = s.CreateVerdict(ctx, sess.ID, traceID, "GET /api/items", "pass", 0.9, "judge", "ok", nil, store.FailureReasonNone)
	_, _ = s.CreateEvidence(ctx, traceID, "response", `{"items":[]}`)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	md := RenderMarkdown(data)
	assert.Contains(t, md, "<details>")
	assert.Contains(t, md, "Evidence")
	assert.Contains(t, md, `{"items":[]}`)
}

func TestRenderHTML_WithEvidence(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "evidence html", "proj")
	require.NoError(t, err)

	traceID, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /api/status")
	_ = s.FinishTrace(ctx, traceID, "pass")
	_, _ = s.CreateVerdict(ctx, sess.ID, traceID, "GET /api/status", "pass", 0.9, "judge", "status ok", nil, store.FailureReasonNone)
	_, _ = s.CreateEvidence(ctx, traceID, "log", "request completed in 50ms")

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	html, err := RenderHTMLString(data)
	require.NoError(t, err)
	assert.Contains(t, html, "Evidence")
	assert.Contains(t, html, "request completed in 50ms")
}

func TestBuildReport_WithAutoTest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "autotest test", "proj")
	require.NoError(t, err)

	// Update with AutoTest report
	testReport := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"target_file": "foo.go",
				"target_func": "Foo",
				"reason":      "0% covered",
				"test_path":   "foo_test.go",
				"status":      "written",
			},
			{
				"target_file": "bar.go",
				"target_func": "Bar",
				"reason":      "no test file",
				"test_path":   "bar_test.go",
				"status":      "reverted",
			},
		},
		"before_coverage_pct": 50.0,
		"after_coverage_pct":  60.0,
	}
	err = s.UpdateSessionAutoTest(ctx, sess.ID, testReport)
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	// Verify AutoTest data is unmarshaled
	require.NotNil(t, data.AutoTest)
	assert.Equal(t, 50.0, data.AutoTest.BeforeCoveragePct)
	assert.Equal(t, 60.0, data.AutoTest.AfterCoveragePct)
	assert.Len(t, data.AutoTest.Items, 2)
	assert.Equal(t, "foo.go", data.AutoTest.Items[0].TargetFile)
	assert.Equal(t, "Foo", data.AutoTest.Items[0].TargetFunc)
	assert.Equal(t, "written", data.AutoTest.Items[0].Status)
	assert.Equal(t, "bar.go", data.AutoTest.Items[1].TargetFile)
	assert.Equal(t, "reverted", data.AutoTest.Items[1].Status)
}

func TestRenderMarkdown_WithAutoTest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "autotest md", "proj")
	require.NoError(t, err)

	// Update with AutoTest report
	testReport := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"target_file": "api/handler.go",
				"target_func": "HandleRequest",
				"reason":      "0% covered",
				"test_path":   "api/handler_test.go",
				"status":      "written",
			},
		},
		"before_coverage_pct": 62.0,
		"after_coverage_pct":  71.5,
	}
	err = s.UpdateSessionAutoTest(ctx, sess.ID, testReport)
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	md := RenderMarkdown(data)
	assert.Contains(t, md, "## AutoTest")
	assert.Contains(t, md, "62.0% → 71.5%")
	assert.Contains(t, md, "Written / Reverted / Skipped / Failed")
	assert.Contains(t, md, "1 / 0 / 0 / 0")
	assert.Contains(t, md, "api/handler_test.go")
	assert.Contains(t, md, "HandleRequest")
	assert.Contains(t, md, "✅ written")
}

func TestRenderMarkdown_NoAutoTest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "no autotest", "proj")
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	md := RenderMarkdown(data)
	assert.NotContains(t, md, "## AutoTest")
	assert.Nil(t, data.AutoTest)
}

func TestRenderHTML_WithAutoTest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "autotest html", "proj")
	require.NoError(t, err)

	// Update with AutoTest report
	testReport := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"target_file": "service/user.go",
				"target_func": "GetUser",
				"reason":      "no test file",
				"test_path":   "service/user_test.go",
				"status":      "written",
			},
		},
		"before_coverage_pct": 45.0,
		"after_coverage_pct":  55.0,
	}
	err = s.UpdateSessionAutoTest(ctx, sess.ID, testReport)
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	html, err := RenderHTMLString(data)
	require.NoError(t, err)
	assert.Contains(t, html, "<h2>AutoTest</h2>")
	assert.Contains(t, html, "45.0% → 55.0%")
	assert.Contains(t, html, "service/user_test.go")
	assert.Contains(t, html, "badge-written")
}

func TestRenderHTML_NoAutoTest(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "no autotest html", "proj")
	require.NoError(t, err)

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	html, err := RenderHTMLString(data)
	require.NoError(t, err)
	assert.NotContains(t, html, "<h2>AutoTest</h2>")
	assert.Nil(t, data.AutoTest)
}

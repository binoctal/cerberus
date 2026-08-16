package report

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRenderJUnit_AllVerdicts(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "junit test", "project")
	require.NoError(t, err)

	trace1, err := s.CreateTrace(ctx, sess.ID, "http", "GET /api/health")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(ctx, trace1, "pass"))

	trace2, err := s.CreateTrace(ctx, sess.ID, "http", "POST /api/users")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(ctx, trace2, "fail"))

	trace3, err := s.CreateTrace(ctx, sess.ID, "http", "GET /api/skip")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(ctx, trace3, "skip"))

	trace4, err := s.CreateTrace(ctx, sess.ID, "http", "GET /api/unknown")
	require.NoError(t, err)
	require.NoError(t, s.FinishTrace(ctx, trace4, "uncertain"))

	_, err = s.CreateVerdict(ctx, sess.ID, trace1, "GET /api/health", "", "pass", 0.99, "judge", "healthy", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, trace2, "POST /api/users", "", "fail", 0.85, "judge", "duplicate email", nil, store.FailureReasonAssertionFailed, false, "", "")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, trace3, "GET /api/skip", "", "skip", 0.0, "judge", "not applicable", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)
	_, err = s.CreateVerdict(ctx, sess.ID, trace4, "GET /api/unknown", "", "uncertain", 0.5, "judge", "unexpected status", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)

	summary := session.SessionSummary{
		TotalCases: 4, Passed: 1, Failed: 1, Skipped: 1, Uncertain: 1,
		DurationMs: 3500,
	}
	require.NoError(t, s.UpdateSessionStats(ctx, sess.ID, 25.0, summary))
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	xmlBytes, err := RenderJUnit(data)
	require.NoError(t, err)
	require.NotNil(t, xmlBytes)

	xmlStr := string(xmlBytes)

	// Valid XML with header.
	assert.True(t, strings.HasPrefix(xmlStr, "<?xml"))

	// Parse to verify structure.
	var doc junitXML
	require.NoError(t, xml.Unmarshal(xmlBytes, &doc))

	require.Len(t, doc.Suites, 1)
	suite := doc.Suites[0]
	assert.Equal(t, 4, suite.Tests)
	assert.Equal(t, 1, suite.Failures)
	assert.Equal(t, 1, suite.Errors)
	assert.Equal(t, 1, suite.Skipped)
	assert.Equal(t, "3.500", suite.Time)
	require.Len(t, suite.Cases, 4)

	// Verify case names are slugified (spaces→_, /→., :→_).
	names := make(map[string]bool)
	for _, tc := range suite.Cases {
		names[tc.Name] = true
	}
	assert.Contains(t, names, "GET_.api.health")
	assert.Contains(t, names, "POST_.api.users")

	// Find specific cases by verdict status.
	for _, tc := range suite.Cases {
		if strings.Contains(tc.Name, "users") {
			require.NotNil(t, tc.Failure, "fail verdict should have <failure>")
			assert.Contains(t, tc.Failure.Message, "FAIL")
			assert.Contains(t, tc.Failure.Contents, "duplicate email")
		}
		if strings.Contains(tc.Name, "unknown") {
			require.NotNil(t, tc.Error, "uncertain verdict should have <error>")
			assert.Contains(t, tc.Error.Message, "UNCERTAIN")
			assert.Contains(t, tc.Error.Contents, "unexpected status")
		}
		if strings.Contains(tc.Name, "skip") {
			require.NotNil(t, tc.Skip, "skip verdict should have <skipped>")
		}
	}
}

func TestRenderJUnit_PassOnly(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "pass only", "project")
	require.NoError(t, err)

	trace1, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /ok")
	require.NoError(t, s.FinishTrace(ctx, trace1, "pass"))
	_, err = s.CreateVerdict(ctx, sess.ID, trace1, "GET /ok", "", "pass", 1.0, "judge", "all good", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)

	require.NoError(t, s.UpdateSessionStats(ctx, sess.ID, 100.0, session.SessionSummary{
		TotalCases: 1, Passed: 1, DurationMs: 500,
	}))
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	xmlBytes, err := RenderJUnit(data)
	require.NoError(t, err)

	var doc junitXML
	require.NoError(t, xml.Unmarshal(xmlBytes, &doc))

	suite := doc.Suites[0]
	assert.Equal(t, 1, suite.Tests)
	assert.Equal(t, 0, suite.Failures)
	assert.Equal(t, 0, suite.Errors)
	assert.Equal(t, 0, suite.Skipped)
	require.Len(t, suite.Cases, 1)
	assert.Nil(t, suite.Cases[0].Failure)
	assert.Nil(t, suite.Cases[0].Error)
	assert.Nil(t, suite.Cases[0].Skip)
}

func TestRenderJUnit_NoVerdicts(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "empty", "project")
	require.NoError(t, err)
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	xmlBytes, err := RenderJUnit(data)
	require.NoError(t, err)

	var doc junitXML
	require.NoError(t, xml.Unmarshal(xmlBytes, &doc))

	suite := doc.Suites[0]
	assert.Equal(t, 1, suite.Tests)
	assert.Equal(t, 1, suite.Skipped)
	require.Len(t, suite.Cases, 1)
	assert.Equal(t, "cerberus.no-results", suite.Cases[0].Name)
	require.NotNil(t, suite.Cases[0].Skip)
	assert.Contains(t, suite.Cases[0].Skip.Message, "no verdicts")
}

func TestRenderJUnit_NoSummary(t *testing.T) {
	data := &ReportData{
		Session: &store.Session{ID: "test-123"},
		Verdicts: []store.Verdict{
			{ID: 1, Target: "GET /api/test", Status: "pass", Confidence: 0.9},
		},
	}

	xmlBytes, err := RenderJUnit(data)
	require.NoError(t, err)

	var doc junitXML
	require.NoError(t, xml.Unmarshal(xmlBytes, &doc))

	suite := doc.Suites[0]
	assert.Equal(t, "", suite.Time, "no summary → no time")
	assert.Equal(t, 1, suite.Tests)
}

func TestVerdictName(t *testing.T) {
	tests := []struct {
		target, want string
	}{
		{"GET /api/health", "GET_.api.health"},
		{"POST http://localhost:8080/create", "POST_http_..localhost_8080.create"},
		{"", "verdict-42"},
	}
	for _, tt := range tests {
		v := store.Verdict{ID: 42, Target: tt.target}
		got := verdictName(v)
		assert.Equal(t, tt.want, got)
	}
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "short", truncate("short", 100))
	assert.Equal(t, strings.Repeat("x", 10)+"... (truncated)", truncate(strings.Repeat("x", 20), 10))
	assert.Equal(t, "", truncate("", 5))
}

func TestRenderJUnit_WithEvidence(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "evidence test", "project")
	require.NoError(t, err)

	// Create trace + fail verdict with evidence.
	trace1, _ := s.CreateTrace(ctx, sess.ID, "http", "POST /api/login")
	require.NoError(t, s.FinishTrace(ctx, trace1, "fail"))
	_, err = s.CreateVerdict(ctx, sess.ID, trace1, "POST /api/login", "", "fail", 0.7, "judge", "auth failed", nil, store.FailureReasonAssertionFailed, false, "", "")
	require.NoError(t, err)

	// Record evidence for the trace.
	_, err = s.CreateEvidence(ctx, trace1, "agent_observation", `{"phase":"steer_attempt","success":false,"summary":"401 Unauthorized"}`)
	require.NoError(t, err)
	_, err = s.CreateEvidence(ctx, trace1, "agent_observation", `{"phase":"recovery","success":false,"summary":"still 401"}`)
	require.NoError(t, err)

	require.NoError(t, s.UpdateSessionStats(ctx, sess.ID, 0.0, session.SessionSummary{
		TotalCases: 1, Failed: 1, DurationMs: 1000,
	}))
	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	// Verify BuildReport loaded evidence.
	require.NotNil(t, data.Evidence)
	assert.Len(t, data.Evidence[trace1], 2)

	// Verify JUnit includes evidence in failure contents.
	xmlBytes, err := RenderJUnit(data)
	require.NoError(t, err)

	var doc junitXML
	require.NoError(t, xml.Unmarshal(xmlBytes, &doc))

	suite := doc.Suites[0]
	require.Len(t, suite.Cases, 1)
	require.NotNil(t, suite.Cases[0].Failure)
	assert.Contains(t, suite.Cases[0].Failure.Contents, "--- Evidence ---")
	assert.Contains(t, suite.Cases[0].Failure.Contents, "agent_observation")
}

func TestEvidenceSummary(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		assert.Equal(t, "", evidenceSummary(nil, 1))
		assert.Equal(t, "", evidenceSummary(map[int64][]store.Evidence{}, 1))
	})

	t.Run("with evidence", func(t *testing.T) {
		evs := map[int64][]store.Evidence{
			10: {
				{Type: "http_response", Content: "200 OK"},
				{Type: "process_output", Content: "exit 0"},
			},
		}
		got := evidenceSummary(evs, 10)
		assert.Contains(t, got, "[http_response] 200 OK")
		assert.Contains(t, got, "[process_output] exit 0")
	})
}

func TestBuildReport_WithEvidence(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "run", "report evidence", "project")
	require.NoError(t, err)

	trace1, _ := s.CreateTrace(ctx, sess.ID, "http", "GET /api/items")
	require.NoError(t, s.FinishTrace(ctx, trace1, "pass"))
	_, err = s.CreateVerdict(ctx, sess.ID, trace1, "GET /api/items", "", "pass", 0.95, "judge", "ok", nil, store.FailureReasonNone, false, "", "")
	require.NoError(t, err)
	_, err = s.CreateEvidence(ctx, trace1, "agent_observation", `{"summary":"200 items returned"}`)
	require.NoError(t, err)

	require.NoError(t, s.UpdateSessionStatus(ctx, sess.ID, "completed"))

	data, err := BuildReport(ctx, s, sess.ID)
	require.NoError(t, err)

	assert.NotNil(t, data.Evidence)
	assert.Len(t, data.Evidence[trace1], 1)
	assert.Equal(t, "agent_observation", data.Evidence[trace1][0].Type)
}

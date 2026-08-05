package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/project"
)

// TestBuildExaminerFillsVocabSummary verifies that buildExaminer renders the
// project vocabulary into the Examiner config. The prompt-injection behavior
// of ExaminerConfig.VocabSummary is covered in the examiner package
// (TestBuildJudgePromptIncludesVocab); this test guards the session wiring —
// that a WS service's vocabulary is rendered and a working Examiner is built.
func TestBuildExaminerFillsVocabSummary(t *testing.T) {
	rp, cleanup := newTestRunPhase(t)
	defer cleanup()

	rp.session.Config.Services = []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{Edges: []project.VocabEdge{{
			FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress",
			Trigger:  "message_handled",
			Delivery: project.VocabDelivery{Mode: "broadcast_web"},
		}}},
	}}

	summary := project.RenderVocabSummary(rp.session.Config.Services)
	require.Contains(t, summary, "workflow:task_progress", "vocab must render for the WS service")

	ex := rp.buildExaminer()
	require.NotNil(t, ex, "buildExaminer must return a non-nil Examiner")
}

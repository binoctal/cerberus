package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
)

// SessionSummary collects statistics across all phases of a session.
type SessionSummary struct {
	Goal       string `json:"goal"`
	ProjectURL string `json:"project_url"`

	// Scout phase.
	EndpointsFound   int `json:"endpoints_found"`
	TestCasesPlanned int `json:"test_cases_planned"`

	// Agent phase.
	TotalCases int `json:"total_cases"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Uncertain  int `json:"uncertain"`

	// Examiner phase.
	Verdicts          int `json:"verdicts"`
	PendingReview     int `json:"pending_review"`
	ReflectionsStored int `json:"reflections_stored"`

	// Coverage.
	CoveragePct float64 `json:"coverage_pct"`

	// Coverage contract and assessment (optional, if coverage enabled)
	Contract   *contract.Contract   `json:"contract,omitempty"`
	Assessment *contract.Assessment `json:"assessment,omitempty"`

	// Resource usage.
	TotalTokens int    `json:"total_tokens"`
	Duration    string `json:"duration"`
	DurationMs  int64  `json:"duration_ms"`
}

// FromResults builds a summary from agent and examiner results.
func FromResults(goal, projectURL string, planCases int, results []agent.StepResult, verdicts []examiner.FinalVerdict, reflections int, tokensUsed int, elapsed time.Duration) *SessionSummary {
	s := &SessionSummary{
		Goal:              goal,
		ProjectURL:        projectURL,
		TestCasesPlanned:  planCases,
		TotalCases:        len(results),
		Verdicts:          len(verdicts),
		ReflectionsStored: reflections,
		TotalTokens:       tokensUsed,
		Duration:          elapsed.Round(time.Millisecond).String(),
		DurationMs:        elapsed.Milliseconds(),
	}

	for _, r := range results {
		switch r.Status {
		case agent.StepPassed:
			s.Passed++
		case agent.StepFailed:
			s.Failed++
		case agent.StepSkipped:
			s.Skipped++
		case agent.StepUncertain:
			s.Uncertain++
		}
	}

	for _, v := range verdicts {
		if v.NeedsReview() {
			s.PendingReview++
		}
	}

	// Compute coverage: passed verdicts / total cases * 100.
	if s.TotalCases > 0 {
		s.CoveragePct = float64(s.Passed) / float64(s.TotalCases) * 100
	}

	return s
}

// String returns a human-readable summary.
func (s *SessionSummary) String() string {
	return fmt.Sprintf(`Session Summary:
  Verdicts: %d pass, %d fail, %d skip, %d uncertain
  Pending review: %d
  Reflections stored: %d (failure + success)
  Total tokens: ~%dK
  Duration: %s`,
		s.Passed, s.Failed, s.Skipped, s.Uncertain,
		s.PendingReview,
		s.ReflectionsStored,
		s.TotalTokens/1000,
		s.Duration)
}

// ToJSON returns the summary as JSON.
func (s *SessionSummary) ToJSON() string {
	b, _ := json.MarshalIndent(s, "", "  ")
	return string(b)
}

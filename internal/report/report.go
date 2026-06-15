package report

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// ReportData assembles all data needed for a session report.
type ReportData struct {
	Session  *store.Session             `json:"session"`
	Traces   []store.Trace              `json:"traces"`
	Verdicts []store.Verdict            `json:"verdicts"`
	Evidence map[int64][]store.Evidence `json:"evidence"` // trace_id → evidence
	Summary  *session.SessionSummary    `json:"summary"`
	AutoTest *autotest.AutoTestReport   `json:"autotest,omitempty"`
}

// BuildReport assembles a full report for the given session.
func BuildReport(ctx context.Context, s *store.Store, sessionID string) (*ReportData, error) {
	sess, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	traces, _ := s.GetTraces(ctx, sessionID)
	verdicts, _ := s.GetVerdicts(ctx, sessionID)

	// Deserialize stats JSON into SessionSummary.
	var summary session.SessionSummary
	if sess.Stats != "" && sess.Stats != "{}" {
		_ = json.Unmarshal([]byte(sess.Stats), &summary)
	}

	if traces == nil {
		traces = []store.Trace{}
	}
	if verdicts == nil {
		verdicts = []store.Verdict{}
	}

	// Load evidence grouped by trace_id.
	evidence, _ := s.GetEvidenceBySession(ctx, sessionID)
	if evidence == nil {
		evidence = make(map[int64][]store.Evidence)
	}

	data := &ReportData{
		Session:  sess,
		Traces:   traces,
		Verdicts: verdicts,
		Evidence: evidence,
		Summary:  &summary,
	}

	// Unmarshal AutoTest report if present.
	if sess.AutoTestReport != "" {
		var atReport autotest.AutoTestReport
		if err := json.Unmarshal([]byte(sess.AutoTestReport), &atReport); err == nil {
			data.AutoTest = &atReport
		}
		// If unmarshal fails, AutoTest remains nil (non-blocking).
	}

	return data, nil
}

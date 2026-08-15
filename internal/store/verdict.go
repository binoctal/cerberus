package store

import (
	"context"
	"time"
)

type Verdict struct {
	ID            int64         `json:"id"`
	SessionID     string        `json:"session_id"`
	TraceID       int64         `json:"trace_id"`
	Target        string        `json:"target"`
	CaseID        string        `json:"case_id,omitempty"` // per-case identity (target is NOT unique per case)
	Status        string        `json:"status"`
	Confidence    float64       `json:"confidence"`
	Source        string        `json:"source"`
	Reasoning     string        `json:"reasoning,omitempty"`
	Suggestions   string        `json:"suggestions,omitempty"`
	FailureReason FailureReason `json:"failure_reason,omitempty"` // Root cause of failure
	Recovered     bool          `json:"recovered,omitempty"`      // True if this verdict is a recovered lazy fallback (A1 Phase 2)
	FallbackFor   string        `json:"fallback_for,omitempty"`   // Non-empty when this verdict is a fallback for another test case
	Replaces      string        `json:"replaces,omitempty"`       // Non-empty when this verdict is a replacement for another test case
	CreatedAt     string        `json:"created_at"`
}

func (s *Store) CreateVerdict(ctx context.Context, sessionID string, traceID int64,
	target, caseID, status string, confidence float64, source, reasoning string, suggestions any, failureReason FailureReason, recovered bool,
	fallbackFor, replaces string) (*Verdict, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO verdicts (session_id, trace_id, target, case_id, status, confidence, source, reasoning, suggestions, failure_reason, recovered, fallback_for, replaces, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, traceID, target, caseID, status, confidence, source, reasoning, jsonText(suggestions), string(failureReason), recovered, fallbackFor, replaces, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Verdict{
		ID: id, SessionID: sessionID, TraceID: traceID,
		Target: target, CaseID: caseID, Status: status, Confidence: confidence,
		Source: source, Reasoning: reasoning, FailureReason: failureReason, Recovered: recovered,
		FallbackFor: fallbackFor, Replaces: replaces, CreatedAt: now,
	}, nil
}

func (s *Store) GetVerdicts(ctx context.Context, sessionID string) ([]Verdict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, trace_id, target, COALESCE(case_id, ''), status, confidence, source,
		        COALESCE(reasoning, ''), COALESCE(suggestions, ''), COALESCE(failure_reason, ''), recovered,
		        COALESCE(fallback_for, ''), COALESCE(replaces, ''), created_at
		 FROM verdicts WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var verdicts []Verdict
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TraceID, &v.Target, &v.CaseID,
			&v.Status, &v.Confidence, &v.Source, &v.Reasoning, &v.Suggestions,
			&v.FailureReason, &v.Recovered, &v.FallbackFor, &v.Replaces, &v.CreatedAt); err != nil {
			return nil, err
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, rows.Err()
}

package store

import (
	"context"
	"time"
)

type Verdict struct {
	ID          int64   `json:"id"`
	SessionID   string  `json:"session_id"`
	TraceID     int64   `json:"trace_id"`
	Target      string  `json:"target"`
	Status      string  `json:"status"`
	Confidence  float64 `json:"confidence"`
	Source      string  `json:"source"`
	Reasoning   string  `json:"reasoning,omitempty"`
	Suggestions string  `json:"suggestions,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Store) CreateVerdict(ctx context.Context, sessionID string, traceID int64,
	target, status string, confidence float64, source, reasoning string, suggestions any) (*Verdict, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO verdicts (session_id, trace_id, target, status, confidence, source, reasoning, suggestions, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, traceID, target, status, confidence, source, reasoning, jsonText(suggestions), now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Verdict{
		ID: id, SessionID: sessionID, TraceID: traceID,
		Target: target, Status: status, Confidence: confidence,
		Source: source, Reasoning: reasoning, CreatedAt: now,
	}, nil
}

func (s *Store) GetVerdicts(ctx context.Context, sessionID string) ([]Verdict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, trace_id, target, status, confidence, source,
		        COALESCE(reasoning, ''), COALESCE(suggestions, ''), created_at
		 FROM verdicts WHERE session_id = ? ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var verdicts []Verdict
	for rows.Next() {
		var v Verdict
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TraceID, &v.Target,
			&v.Status, &v.Confidence, &v.Source, &v.Reasoning, &v.Suggestions,
			&v.CreatedAt); err != nil {
			return nil, err
		}
		verdicts = append(verdicts, v)
	}
	return verdicts, rows.Err()
}

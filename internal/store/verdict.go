package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Verdict struct {
	ID          int64     `json:"id"`
	SessionID   string    `json:"session_id"`
	TraceID     int64     `json:"trace_id"`
	Target      string    `json:"target"`
	Status      string    `json:"status"`
	Confidence  float64   `json:"confidence"`
	Source      string    `json:"source"`
	Reasoning   string    `json:"reasoning,omitempty"`
	Suggestions any       `json:"suggestions,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) CreateVerdict(ctx context.Context, sessionID uuid.UUID, traceID int64,
	target, status string, confidence float64, source, reasoning string, suggestions any) (*Verdict, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO verdicts (session_id, trace_id, target, status, confidence, source, reasoning, suggestions)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		sessionID, traceID, target, status, confidence, source, reasoning, jsonb(suggestions)).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &Verdict{
		ID: id, SessionID: sessionID.String(), TraceID: traceID,
		Target: target, Status: status, Confidence: confidence,
		Source: source, Reasoning: reasoning, Suggestions: suggestions,
	}, nil
}

func (s *Store) GetVerdicts(ctx context.Context, sessionID uuid.UUID) ([]Verdict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, trace_id, target, status, confidence, source, reasoning, suggestions, created_at
		 FROM verdicts WHERE session_id = $1 ORDER BY created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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

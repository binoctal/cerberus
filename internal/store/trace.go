package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Trace struct {
	ID         int64      `json:"id"`
	SessionID  string     `json:"session_id"`
	Category   string     `json:"category"`
	Target     string     `json:"target"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Metadata   any        `json:"metadata,omitempty"`
}

func (s *Store) CreateTrace(ctx context.Context, sessionID uuid.UUID, category, target string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO traces (session_id, category, target) VALUES ($1, $2, $3) RETURNING id`,
		sessionID, category, target).Scan(&id)
	return id, err
}

func (s *Store) FinishTrace(ctx context.Context, id int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE traces SET status = $1, finished_at = NOW() WHERE id = $2`,
		status, id)
	return err
}

func (s *Store) GetTraces(ctx context.Context, sessionID uuid.UUID) ([]Trace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, category, target, status, started_at, finished_at, metadata
		 FROM traces WHERE session_id = $1 ORDER BY started_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var traces []Trace
	for rows.Next() {
		var tr Trace
		if err := rows.Scan(&tr.ID, &tr.SessionID, &tr.Category, &tr.Target,
			&tr.Status, &tr.StartedAt, &tr.FinishedAt, &tr.Metadata); err != nil {
			return nil, err
		}
		traces = append(traces, tr)
	}
	return traces, rows.Err()
}

package store

import (
	"context"
	"time"
)

type Trace struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Category   string `json:"category"`
	Target     string `json:"target"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at,omitempty"`
	Metadata   string `json:"metadata,omitempty"`
}

func (s *Store) CreateTrace(ctx context.Context, sessionID string, category, target string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO traces (session_id, category, target, started_at) VALUES (?, ?, ?, ?)`,
		sessionID, category, target, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) FinishTrace(ctx context.Context, id int64, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE traces SET status = ?, finished_at = ? WHERE id = ?`,
		status, now, id)
	return err
}

func (s *Store) GetTraces(ctx context.Context, sessionID string) ([]Trace, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, category, target, status, started_at, COALESCE(finished_at, ''), COALESCE(metadata, '')
		 FROM traces WHERE session_id = ? ORDER BY started_at`, sessionID)
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

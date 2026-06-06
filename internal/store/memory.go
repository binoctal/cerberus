package store

import (
	"context"
	"time"
)

type EpisodicMemory struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"session_id"`
	Target     string `json:"target"`
	Status     string `json:"status"`
	Verdict    string `json:"verdict,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	CreatedAt  string `json:"created_at"`
}

func (s *Store) RecordEpisodic(ctx context.Context, sessionID string,
	target, status string, verdict any, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_episodic (session_id, target, status, verdict, duration_ms)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, target, status, jsonText(verdict), duration.Milliseconds())
	return err
}

func (s *Store) GetEpisodicByTarget(ctx context.Context, target string, limit int) ([]EpisodicMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, target, status, COALESCE(verdict, ''), COALESCE(duration_ms, 0), created_at
		 FROM memory_episodic WHERE target = ? ORDER BY created_at DESC LIMIT ?`,
		target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []EpisodicMemory
	for rows.Next() {
		var m EpisodicMemory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Target, &m.Status,
			&m.Verdict, &m.DurationMs, &m.CreatedAt); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

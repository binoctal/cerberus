package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type EpisodicMemory struct {
	ID        int64     `json:"id"`
	SessionID string    `json:"session_id"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	Verdict   any       `json:"verdict,omitempty"`
	Duration  string    `json:"duration,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) RecordEpisodic(ctx context.Context, sessionID uuid.UUID,
	target, status string, verdict any, duration time.Duration) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_episodic (session_id, target, status, verdict, duration)
		 VALUES ($1, $2, $3, $4, $5)`,
		sessionID, target, status, jsonb(verdict), duration)
	return err
}

func (s *Store) GetEpisodicByTarget(ctx context.Context, target string, limit int) ([]EpisodicMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, target, status, verdict, duration, created_at
		 FROM memory_episodic WHERE target = $1 ORDER BY created_at DESC LIMIT $2`,
		target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []EpisodicMemory
	for rows.Next() {
		var m EpisodicMemory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Target, &m.Status,
			&m.Verdict, &m.Duration, &m.CreatedAt); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

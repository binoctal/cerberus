package store

import (
	"context"
	"time"
)

// Evidence represents collected data linked to a trace.
type Evidence struct {
	ID        int64  `json:"id"`
	TraceID   int64  `json:"trace_id"`
	Type      string `json:"type"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// CreateEvidence records evidence linked to a trace.
func (s *Store) CreateEvidence(ctx context.Context, traceID int64, evType, content string) (*Evidence, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO evidence (trace_id, type, content, created_at) VALUES (?, ?, ?, ?)`,
		traceID, evType, content, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &Evidence{ID: id, TraceID: traceID, Type: evType, Content: content, CreatedAt: now}, nil
}

// GetEvidenceBySession returns all evidence for a session, grouped by trace_id.
func (s *Store) GetEvidenceBySession(ctx context.Context, sessionID string) (map[int64][]Evidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.trace_id, e.type, e.content, e.created_at
		 FROM evidence e
		 JOIN traces t ON e.trace_id = t.id
		 WHERE t.session_id = ?
		 ORDER BY e.created_at`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[int64][]Evidence)
	for rows.Next() {
		var e Evidence
		if err := rows.Scan(&e.ID, &e.TraceID, &e.Type, &e.Content, &e.CreatedAt); err != nil {
			return nil, err
		}
		result[e.TraceID] = append(result[e.TraceID], e)
	}
	return result, rows.Err()
}

// GetEvidenceByTrace returns all evidence for a given trace, ordered chronologically.
func (s *Store) GetEvidenceByTrace(ctx context.Context, traceID int64) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, trace_id, type, content, created_at
		 FROM evidence WHERE trace_id = ? ORDER BY created_at`, traceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var evidence []Evidence
	for rows.Next() {
		var e Evidence
		if err := rows.Scan(&e.ID, &e.TraceID, &e.Type, &e.Content, &e.CreatedAt); err != nil {
			return nil, err
		}
		evidence = append(evidence, e)
	}
	return evidence, rows.Err()
}

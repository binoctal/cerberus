package store

import (
	"context"
	"time"
)

// RecordMemoryUsage records that a procedural memory was recalled for a case.
// Idempotent via UNIQUE(session_id, case_id, procedural_id).
func (s *Store) RecordMemoryUsage(ctx context.Context, proceduralID int64, sessionID, caseID, target string, attempt int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO memory_usage (procedural_id, session_id, case_id, target, attempt, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		proceduralID, sessionID, caseID, target, attempt, now)
	return err
}

// UnconsolidatedUsage returns unconsolidated memory_usage rows for the given session.
func (s *Store) UnconsolidatedUsage(ctx context.Context, sessionID string) ([]MemoryUsage, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, procedural_id, session_id, case_id, target, COALESCE(attempt,0), created_at
		 FROM memory_usage WHERE session_id=? AND consolidated_at IS NULL`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MemoryUsage
	for rows.Next() {
		var m MemoryUsage
		if err := rows.Scan(&m.ID, &m.ProceduralID, &m.SessionID, &m.CaseID, &m.Target, &m.Attempt, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkUsageConsolidated stamps consolidated_at for rows by id.
func (s *Store) MarkUsageConsolidated(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE memory_usage SET consolidated_at=? WHERE id=?`, now, id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// MemoryUsage represents a row in the memory_usage table.
type MemoryUsage struct {
	ID           int64
	ProceduralID int64
	SessionID    string
	CaseID       string
	Target       string
	Attempt      int
	CreatedAt    string
}

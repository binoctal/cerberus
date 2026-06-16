package store

import (
	"context"
	"time"
)

// ArchiveProcedural marks a procedural memory as archived (no longer injected).
func (s *Store) ArchiveProcedural(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural SET archived = 1 WHERE id = ?`, id)
	return err
}

// MarkStaleProcedural marks old, low-effectiveness memories as stale.
// Stale memories are not injected but are retained for potential effectiveness recovery.
func (s *Store) MarkStaleProcedural(ctx context.Context, expiryDays int, minEffectiveness float64) (int, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -expiryDays).Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural
		 SET archived = 1
		 WHERE created_at < ? AND effectiveness < ? AND COALESCE(archived, 0) = 0`,
		cutoff, minEffectiveness)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AutoArchiveLowEffectiveness archives memories that have fallen below threshold.
func (s *Store) AutoArchiveLowEffectiveness(ctx context.Context, threshold float64) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural SET archived = 1
		 WHERE effectiveness < ? AND COALESCE(archived, 0) = 0`, threshold)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

package store

import (
	"context"
	"fmt"
)

// UpdateProceduralEffectiveness applies EMA update to a procedural memory entry.
// α=0.3: new = 0.7*old + 0.3*(1.0 if success, 0.0 if failure)
func (s *Store) UpdateProceduralEffectiveness(ctx context.Context, id int64, success bool) error {
	var current float64
	if err := s.db.QueryRowContext(ctx,
		`SELECT effectiveness FROM memory_procedural WHERE id = ?`, id).Scan(&current); err != nil {
		return fmt.Errorf("read effectiveness: %w", err)
	}

	var outcome float64
	if success {
		outcome = 1.0
	}
	newEff := 0.7*current + 0.3*outcome

	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural SET effectiveness = ?, usage_count = usage_count + 1 WHERE id = ?`,
		newEff, id)
	return err
}

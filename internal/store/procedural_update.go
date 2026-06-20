package store

import (
	"context"
)

// ApplyProceduralEffectiveness applies ONE atomic EMA update for a procedural
// memory (α=0.3). Replaces the read-modify-write version. `signal` is the
// pass-fraction in [0,1] for this session; `usageDelta` is the number of
// recalled cases that contributed. Call once per (session, procedural_id).
func (s *Store) ApplyProceduralEMA(ctx context.Context, id int64, signal float64, usageDelta int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_procedural
		 SET effectiveness = effectiveness*0.7 + 0.3*?,
		     usage_count = usage_count + ?
		 WHERE id = ?`, signal, usageDelta, id)
	return err
}

// UpdateProceduralEffectiveness kept as a thin wrapper (single-case, signal 1/0)
// for any external caller; consolidate uses ApplyProceduralEMA directly.
func (s *Store) UpdateProceduralEffectiveness(ctx context.Context, id int64, success bool) error {
	sig := 0.0
	if success {
		sig = 1.0
	}
	return s.ApplyProceduralEMA(ctx, id, sig, 1)
}

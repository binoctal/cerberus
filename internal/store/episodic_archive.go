package store

import (
	"context"
	"strconv"
)

// ArchiveStaleEpisodic archives episodic memories older than maxAgeDays.
func (s *Store) ArchiveStaleEpisodic(ctx context.Context, maxAgeDays int) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE memory_episodic SET archived = 1
			 WHERE COALESCE(archived,0)=0
			   AND created_at <= datetime('now', ?)`,
		"-"+strconv.Itoa(maxAgeDays)+" days")
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID          string  `json:"id"`
	Mode        string  `json:"mode"`
	Status      string  `json:"status"`
	Goal        string  `json:"goal,omitempty"`
	ProjectName string  `json:"project_name,omitempty"`
	CoveragePct float64 `json:"coverage_pct"`
	Stats       string  `json:"stats"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  string  `json:"finished_at,omitempty"`
}

func (s *Store) CreateSession(ctx context.Context, mode, goal, projectName string) (*Session, error) {
	id := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, mode, goal, project_name, started_at) VALUES (?, ?, ?, ?, ?)`,
		id, mode, goal, projectName, now)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID: id, Mode: mode, Status: "running",
		Goal: goal, ProjectName: projectName,
		StartedAt: now, Stats: "{}",
	}, nil
}

func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, COALESCE(finished_at, '')
		 FROM sessions WHERE id = ?`, id).Scan(
		&sess.ID, &sess.Mode, &sess.Status, &sess.Goal, &sess.ProjectName,
		&sess.CoveragePct, &sess.Stats, &sess.StartedAt, &sess.FinishedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, COALESCE(finished_at, '')
		 FROM sessions ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Mode, &sess.Status, &sess.Goal,
			&sess.ProjectName, &sess.CoveragePct, &sess.Stats,
			&sess.StartedAt, &sess.FinishedAt); err != nil {
			return nil, err
		}
		sessions = append(sessions, sess)
	}
	return sessions, rows.Err()
}

func (s *Store) UpdateSessionStatus(ctx context.Context, id string, status string) error {
	isTerminal := false
	for _, t := range []string{"completed", "failed", "aborted"} {
		if status == t {
			isTerminal = true
		}
	}
	if isTerminal {
		now := time.Now().UTC().Format(time.RFC3339)
		_, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET status = ?, finished_at = ? WHERE id = ?`,
			status, now, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status = ? WHERE id = ?`, status, id)
	return err
}

// UpdateSessionStats writes final stats and coverage to a completed session.
func (s *Store) UpdateSessionStats(ctx context.Context, id string, coveragePct float64, stats any) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET coverage_pct = ?, stats = ? WHERE id = ?`,
		coveragePct, jsonText(stats), id)
	return err
}

package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID          uuid.UUID  `json:"id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Goal        string     `json:"goal,omitempty"`
	ProjectName string     `json:"project_name,omitempty"`
	CoveragePct float64    `json:"coverage_pct"`
	Stats       any        `json:"stats"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

func (s *Store) CreateSession(ctx context.Context, mode, goal, projectName string) (*Session, error) {
	id := uuid.New()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, mode, goal, project_name) VALUES ($1, $2, $3, $4)`,
		id, mode, goal, projectName)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID: id, Mode: mode, Status: "running",
		Goal: goal, ProjectName: projectName,
		StartedAt: time.Now(),
	}, nil
}

func (s *Store) GetSession(ctx context.Context, id uuid.UUID) (*Session, error) {
	sess := &Session{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, finished_at
		 FROM sessions WHERE id = $1`, id).Scan(
		&sess.ID, &sess.Mode, &sess.Status, &sess.Goal, &sess.ProjectName,
		&sess.CoveragePct, &sess.Stats, &sess.StartedAt, &sess.FinishedAt)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Store) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, mode, status, goal, project_name, coverage_pct, stats, started_at, finished_at
		 FROM sessions ORDER BY started_at DESC LIMIT $1`, limit)
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

func (s *Store) UpdateSessionStatus(ctx context.Context, id uuid.UUID, status string) error {
	isTerminal := false
	for _, t := range []string{"completed", "failed", "aborted"} {
		if status == t {
			isTerminal = true
		}
	}
	if isTerminal {
		_, err := s.db.ExecContext(ctx,
			`UPDATE sessions SET status = $1, finished_at = NOW() WHERE id = $2`,
			status, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET status = $1 WHERE id = $2`, status, id)
	return err
}

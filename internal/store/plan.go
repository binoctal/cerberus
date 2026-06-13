package store

import (
	"context"
	"encoding/json"
	"time"
)

// SessionPlan stores a serialized test plan for resumption.
type SessionPlan struct {
	SessionID string `json:"session_id"`
	PlanJSON  string `json:"plan_json"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// SavePlan persists a test plan for a session. Uses UPSERT.
func (s *Store) SavePlan(ctx context.Context, sessionID string, plan any) error {
	planJSON, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO session_plans (session_id, plan_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(session_id) DO UPDATE SET plan_json = excluded.plan_json, updated_at = excluded.updated_at`,
		sessionID, string(planJSON), now, now)
	return err
}

// LoadPlan deserializes the test plan for a session into dest.
func (s *Store) LoadPlan(ctx context.Context, sessionID string, dest any) error {
	var planJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT plan_json FROM session_plans WHERE session_id = ?`, sessionID).Scan(&planJSON)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(planJSON), dest)
}

// GetCompletedTargets returns the set of targets that have verdicts in this session.
func (s *Store) GetCompletedTargets(ctx context.Context, sessionID string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT target FROM verdicts WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	completed := make(map[string]bool)
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			return nil, err
		}
		completed[target] = true
	}
	return completed, rows.Err()
}

// HasPlan returns true if a session has a saved plan.
func (s *Store) HasPlan(ctx context.Context, sessionID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_plans WHERE session_id = ?`, sessionID).Scan(&count)
	return count > 0, err
}

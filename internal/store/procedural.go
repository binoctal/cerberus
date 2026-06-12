package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ProceduralMemory represents an L3 learned strategy.
type ProceduralMemory struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Condition     string  `json:"condition"`
	Action        string  `json:"action"`
	Effectiveness float64 `json:"effectiveness"`
	UsageCount    int     `json:"usage_count"`
	ProjectName   string  `json:"project_name,omitempty"`
	Category      string  `json:"category"`
	Type          string  `json:"type"` // "failure" or "success"
	Archived      bool    `json:"archived"`
	CreatedAt     string  `json:"created_at"`
}

// StoreProcedural records a new L3 procedural memory entry.
func (s *Store) StoreProcedural(ctx context.Context, name, condition, action, projectName string) (*ProceduralMemory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural (name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		 VALUES (?, ?, ?, 0.5, 0, ?, ?, 'failure', 0, ?)`,
		name, condition, action, projectName, name, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ProceduralMemory{
		ID: id, Name: name, Condition: condition, Action: action,
		Effectiveness: 0.5, ProjectName: projectName, Category: name,
		Type: "failure", CreatedAt: now,
	}, nil
}

// StoreProceduralWithType records a new L3 procedural memory with explicit type and category.
func (s *Store) StoreProceduralWithType(ctx context.Context, name, condition, action, projectName, category, refType string) (*ProceduralMemory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural (name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at)
		 VALUES (?, ?, ?, 0.5, 0, ?, ?, ?, 0, ?)`,
		name, condition, action, projectName, category, refType, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ProceduralMemory{
		ID: id, Name: name, Condition: condition, Action: action,
		Effectiveness: 0.5, ProjectName: projectName, Category: category,
		Type: refType, CreatedAt: now,
	}, nil
}

// GetProceduralByMatch finds L3 memories relevant to a target using substring matching.
// Condition patterns may contain glob-style *; they are stripped for substring comparison.
// Returns non-archived entries with effectiveness >= 0.2, ordered by effectiveness DESC.
func (s *Store) GetProceduralByMatch(ctx context.Context, target string, limit int) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name, ''), COALESCE(category, 'general_failure'),
		        COALESCE(type, 'failure'), COALESCE(archived, 0), created_at
		 FROM memory_procedural
		 WHERE effectiveness >= 0.2 AND COALESCE(archived, 0) = 0
		 ORDER BY effectiveness DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var all []ProceduralMemory
	for rows.Next() {
		var m ProceduralMemory
		var archived int
		if err := rows.Scan(&m.ID, &m.Name, &m.Condition, &m.Action,
			&m.Effectiveness, &m.UsageCount, &m.ProjectName, &m.Category,
			&m.Type, &archived, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Archived = archived == 1
		all = append(all, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Filter: match if condition (stripped of *) is a substring of target or vice versa.
	var matched []ProceduralMemory
	for _, m := range all {
		condStripped := strings.ReplaceAll(m.Condition, "*", "")
		if condStripped == "" {
			continue
		}
		if strings.Contains(target, condStripped) || strings.Contains(condStripped, target) {
			matched = append(matched, m)
			if len(matched) >= limit {
				break
			}
		}
	}
	return matched, nil
}

// GetProceduralByEffectiveness returns all non-archived L3 memories with effectiveness >= threshold.
func (s *Store) GetProceduralByEffectiveness(ctx context.Context, threshold float64, limit int) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name, ''), COALESCE(category, 'general_failure'),
		        COALESCE(type, 'failure'), COALESCE(archived, 0), created_at
		 FROM memory_procedural
		 WHERE effectiveness >= ? AND COALESCE(archived, 0) = 0
		 ORDER BY effectiveness DESC
		 LIMIT ?`, threshold, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []ProceduralMemory
	for rows.Next() {
		var m ProceduralMemory
		var archived int
		if err := rows.Scan(&m.ID, &m.Name, &m.Condition, &m.Action,
			&m.Effectiveness, &m.UsageCount, &m.ProjectName, &m.Category,
			&m.Type, &archived, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Archived = archived == 1
		results = append(results, m)
	}
	return results, rows.Err()
}

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

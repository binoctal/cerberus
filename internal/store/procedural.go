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
	CreatedAt     string  `json:"created_at"`
}

// StoreProcedural records a new L3 procedural memory entry.
func (s *Store) StoreProcedural(ctx context.Context, name, condition, action, projectName string) (*ProceduralMemory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural (name, condition, action, effectiveness, usage_count, project_name, created_at)
		 VALUES (?, ?, ?, 0.5, 0, ?, ?)`,
		name, condition, action, projectName, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &ProceduralMemory{
		ID: id, Name: name, Condition: condition, Action: action,
		Effectiveness: 0.5, ProjectName: projectName, CreatedAt: now,
	}, nil
}

// GetProceduralByMatch finds L3 memories relevant to a target using substring matching.
// Condition patterns may contain glob-style *; they are stripped for substring comparison.
// Returns entries with effectiveness >= 0.2, ordered by effectiveness DESC, limited to n.
func (s *Store) GetProceduralByMatch(ctx context.Context, target string, limit int) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name, ''), created_at
		 FROM memory_procedural
		 WHERE effectiveness >= 0.2
		 ORDER BY effectiveness DESC`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var all []ProceduralMemory
	for rows.Next() {
		var m ProceduralMemory
		if err := rows.Scan(&m.ID, &m.Name, &m.Condition, &m.Action,
			&m.Effectiveness, &m.UsageCount, &m.ProjectName, &m.CreatedAt); err != nil {
			return nil, err
		}
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

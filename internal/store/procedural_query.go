package store

import (
	"context"
	"strings"
)

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

package store

import (
	"context"
	"database/sql"
	"strings"
)

// scanProcedural is a shared row scanner for ProceduralMemory queries.
// Populates Embedding via ParseEmbedding and EmbeddingModel.
func scanProcedural(row *sql.Row) (*ProceduralMemory, error) {
	var m ProceduralMemory
	var archived int
	var embStr, embModel string
	if err := row.Scan(&m.ID, &m.Name, &m.Condition, &m.Action,
		&m.Effectiveness, &m.UsageCount, &m.ProjectName, &m.Category,
		&m.Type, &archived, &m.CreatedAt, &embStr, &embModel); err != nil {
		return nil, err
	}
	m.Archived = archived == 1
	var err error
	m.Embedding, err = ParseEmbedding(embStr)
	if err != nil {
		m.Embedding = nil
	}
	m.EmbeddingModel = embModel
	return &m, nil
}

// scanProceduralRows scans multiple rows from a Query result.
func scanProceduralRows(rows *sql.Rows) ([]ProceduralMemory, error) {
	var all []ProceduralMemory
	for rows.Next() {
		m, err := scanProceduralFromRows(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, *m)
	}
	return all, nil
}

// scanProceduralFromRows scans a single row from sql.Rows.
func scanProceduralFromRows(rows *sql.Rows) (*ProceduralMemory, error) {
	var m ProceduralMemory
	var archived int
	var embStr, embModel string
	if err := rows.Scan(&m.ID, &m.Name, &m.Condition, &m.Action,
		&m.Effectiveness, &m.UsageCount, &m.ProjectName, &m.Category,
		&m.Type, &archived, &m.CreatedAt, &embStr, &embModel); err != nil {
		return nil, err
	}
	m.Archived = archived == 1
	var err error
	m.Embedding, err = ParseEmbedding(embStr)
	if err != nil {
		m.Embedding = nil
	}
	m.EmbeddingModel = embModel
	return &m, nil
}

// GetProceduralByMatch finds L3 memories relevant to a target using substring matching.
// Condition patterns may contain glob-style *; they are stripped for substring comparison.
// Returns non-archived entries with effectiveness >= 0.2, ordered by effectiveness DESC.
func (s *Store) GetProceduralByMatch(ctx context.Context, target string, limit int) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name, ''), COALESCE(category, 'general_failure'),
		        COALESCE(type, 'failure'), COALESCE(archived, 0), created_at,
		        COALESCE(embedding, '[]'), COALESCE(embedding_model, '')
		 FROM memory_procedural
		 WHERE effectiveness >= 0.2 AND COALESCE(archived, 0) = 0
		 ORDER BY effectiveness DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	all, err := scanProceduralRows(rows)
	if err != nil {
		return nil, err
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
		        COALESCE(type, 'failure'), COALESCE(archived, 0), created_at,
		        COALESCE(embedding, '[]'), COALESCE(embedding_model, '')
		 FROM memory_procedural
		 WHERE effectiveness >= ? AND COALESCE(archived, 0) = 0
		 ORDER BY effectiveness DESC
		 LIMIT ?`, threshold, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	results, err := scanProceduralRows(rows)
	if err != nil {
		return nil, err
	}
	return results, rows.Err()
}

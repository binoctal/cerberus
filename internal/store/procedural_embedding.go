package store

import (
	"context"
	"sort"
)

// GetProceduralByEmbedding recalls non-archived procedural memories for a project
// whose embedding_model matches `model`, ranked by cosine similarity to the
// query (>= threshold), re-ranked by effectiveness. Mirrors SearchSemanticForProject.
func (s *Store) GetProceduralByEmbedding(ctx context.Context, queryEmbedding []float64, project string, topK int, threshold float64, model string) ([]ProceduralMemory, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name,''), COALESCE(category,'general_failure'),
		        COALESCE(type,'failure'), COALESCE(archived,0), created_at,
		        COALESCE(embedding,'[]'), COALESCE(embedding_model,'')
		 FROM memory_procedural
		 WHERE COALESCE(archived,0)=0 AND effectiveness >= 0.2
		   AND COALESCE(embedding_model,'') = ? AND COALESCE(embedding,'[]') != '[]'`,
		model)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	type scored struct {
		m   ProceduralMemory
		sim float64
	}
	var hits []scored
	for rows.Next() {
		m, err := scanProceduralFromRows(rows)
		if err != nil {
			return nil, err
		}
		if m.ProjectName != project && m.ProjectName != "" {
			continue
		}
		sim := CosineSimilarity(queryEmbedding, m.Embedding)
		if sim >= threshold {
			hits = append(hits, scored{m: *m, sim: sim})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool {
		// Re-rank by effectiveness, tiebreak similarity.
		if hits[i].m.Effectiveness != hits[j].m.Effectiveness {
			return hits[i].m.Effectiveness > hits[j].m.Effectiveness
		}
		return hits[i].sim > hits[j].sim
	})
	if topK > 0 && len(hits) > topK {
		hits = hits[:topK]
	}
	out := make([]ProceduralMemory, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out, nil
}

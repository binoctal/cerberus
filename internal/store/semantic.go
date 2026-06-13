package store

import (
	"context"
	"encoding/json"
	"sort"
	"time"
)

// SemanticMemory represents an L2 semantic memory record.
type SemanticMemory struct {
	ID             int64     `json:"id"`
	Content        string    `json:"content"`
	Source         string    `json:"source"`
	Tags           []string  `json:"tags"`
	Confidence     float64   `json:"confidence"`
	ProjectName    string    `json:"project_name,omitempty"`
	Embedding      []float64 `json:"embedding,omitempty"`
	EmbeddingModel string    `json:"embedding_model,omitempty"`
	CreatedAt      string    `json:"created_at"`
	UpdatedAt      string    `json:"updated_at"`
}

// SemanticSearchResult is a memory with its similarity score.
type SemanticSearchResult struct {
	SemanticMemory
	Score float64 `json:"score"`
}

// StoreSemantic inserts a new semantic memory record.
func (s *Store) StoreSemantic(ctx context.Context, content, source, project string,
	tags []string, embedding []float64, model string) (int64, error) {

	tagsJSON, _ := json.Marshal(tags)
	embJSON := FormatEmbedding(embedding)

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO memory_semantic (content, source, tags, confidence, project_name, embedding, embedding_model)
		 VALUES (?, ?, ?, 0.5, ?, ?, ?)`,
		content, source, string(tagsJSON), project, embJSON, model)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSemanticByID retrieves a single semantic memory by ID.
func (s *Store) GetSemanticByID(ctx context.Context, id int64) (*SemanticMemory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, content, source, tags, confidence, COALESCE(project_name, ''),
		        COALESCE(embedding, '[]'), COALESCE(embedding_model, ''), created_at, updated_at
		 FROM memory_semantic WHERE id = ?`, id)

	var m SemanticMemory
	var tagsJSON, embJSON string
	if err := row.Scan(&m.ID, &m.Content, &m.Source, &tagsJSON, &m.Confidence,
		&m.ProjectName, &embJSON, &m.EmbeddingModel, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
	m.Embedding, _ = ParseEmbedding(embJSON)
	return &m, nil
}

// SearchSemantic performs brute-force cosine similarity search against stored embeddings.
// Returns results sorted by descending similarity, filtered by threshold.
func (s *Store) SearchSemantic(ctx context.Context, queryEmbedding []float64,
	limit int, threshold float64) ([]SemanticSearchResult, error) {

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, content, source, tags, confidence, COALESCE(project_name, ''),
		        COALESCE(embedding, '[]'), COALESCE(embedding_model, ''), created_at, updated_at
		 FROM memory_semantic`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var results []SemanticSearchResult
	for rows.Next() {
		var m SemanticMemory
		var tagsJSON, embJSON string
		if err := rows.Scan(&m.ID, &m.Content, &m.Source, &tagsJSON, &m.Confidence,
			&m.ProjectName, &embJSON, &m.EmbeddingModel, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
		m.Embedding, _ = ParseEmbedding(embJSON)

		if len(m.Embedding) == 0 {
			continue
		}
		score := CosineSimilarity(queryEmbedding, m.Embedding)
		if score >= threshold {
			results = append(results, SemanticSearchResult{SemanticMemory: m, Score: score})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Sort by descending score.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// DeleteSemantic removes a semantic memory by ID.
func (s *Store) DeleteSemantic(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM memory_semantic WHERE id = ?`, id)
	return err
}

// UpdateSemanticTimestamp updates the updated_at field.
func (s *Store) UpdateSemanticTimestamp(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE memory_semantic SET updated_at = ? WHERE id = ?`,
		time.Now().UTC().Format("2006-01-02 15:04:05"), id)
	return err
}

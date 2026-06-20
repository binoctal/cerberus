package store

import (
	"context"
	"encoding/json"
	"time"
)

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

// StoreProceduralWithType upserts a procedural memory keyed by
// (project_name, condition, action). On conflict it refreshes category/type and
// the embedding (always re-embedded; trigram is cheap) but PRESERVES
// effectiveness and usage_count so the EMA is never wiped.
func (s *Store) StoreProceduralWithType(ctx context.Context, name, condition, action, projectName, category, refType string, embedding []float64, model string) (*ProceduralMemory, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	embJSON, err := json.Marshal(embedding)
	if err != nil {
		embJSON = []byte("[]")
	}
	// Upsert: new rows get effectiveness 0.5, usage_count 0; existing rows keep theirs.
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO memory_procedural
		   (name, condition, action, effectiveness, usage_count, project_name, category, type, archived, created_at, embedding, embedding_model)
		 VALUES (?, ?, ?, 0.5, 0, ?, ?, ?, 0, ?, ?, ?)
		 ON CONFLICT(project_name, condition, action) DO UPDATE SET
		   name = excluded.name, category = excluded.category, type = excluded.type,
		   embedding = excluded.embedding, embedding_model = excluded.embedding_model`,
		name, condition, action, projectName, category, refType, now, string(embJSON), model)
	if err != nil {
		return nil, err
	}
	return s.GetProceduralByExactKey(ctx, projectName, condition, action)
}

// GetProceduralByExactKey loads the row for a (project, condition, action) triple.
func (s *Store) GetProceduralByExactKey(ctx context.Context, projectName, condition, action string) (*ProceduralMemory, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, condition, action, effectiveness, usage_count,
		        COALESCE(project_name,''), COALESCE(category,'general_failure'),
		        COALESCE(type,'failure'), COALESCE(archived,0), created_at,
		        COALESCE(embedding,'[]'), COALESCE(embedding_model,'')
		 FROM memory_procedural WHERE project_name=? AND condition=? AND action=?`,
		projectName, condition, action)
	return scanProcedural(row)
}

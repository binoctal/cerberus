package store

import (
	"context"
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

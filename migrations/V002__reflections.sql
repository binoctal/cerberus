-- V002: Add category, type columns to memory_procedural for Reflexion support.
-- Also adds archived flag for effectiveness-based lifecycle management.

ALTER TABLE memory_procedural ADD COLUMN category TEXT NOT NULL DEFAULT 'general_failure';
ALTER TABLE memory_procedural ADD COLUMN type TEXT NOT NULL DEFAULT 'failure';
ALTER TABLE memory_procedural ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

-- Index for effectiveness-based queries (strategy matching).
CREATE INDEX IF NOT EXISTS idx_procedural_effectiveness ON memory_procedural(effectiveness DESC);
CREATE INDEX IF NOT EXISTS idx_procedural_project ON memory_procedural(project_name);

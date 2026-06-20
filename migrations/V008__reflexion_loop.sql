-- migrations/V008__reflexion_loop.sql
-- L3 embedding recall + dedup, L1/L2 archival, effectiveness feedback.

ALTER TABLE memory_procedural ADD COLUMN embedding TEXT NOT NULL DEFAULT '[]';
ALTER TABLE memory_procedural ADD COLUMN embedding_model TEXT NOT NULL DEFAULT '';

-- Dedup existing duplicate (project_name, condition, action) rows BEFORE the
-- unique index: keep newest by created_at, tiebreak usage_count DESC.
DELETE FROM memory_procedural WHERE id NOT IN (
  SELECT id FROM (
    SELECT id, ROW_NUMBER() OVER (
      PARTITION BY project_name, condition, action
      ORDER BY created_at DESC, usage_count DESC
    ) AS rn FROM memory_procedural
  ) WHERE rn = 1
);
CREATE UNIQUE INDEX idx_procedural_dedup ON memory_procedural(project_name, condition, action);

ALTER TABLE memory_episodic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;
ALTER TABLE memory_semantic ADD COLUMN archived INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS memory_usage (
  id              INTEGER PRIMARY KEY AUTOINCREMENT,
  procedural_id   INTEGER NOT NULL,
  session_id      TEXT    NOT NULL,
  case_id         TEXT    NOT NULL,
  target          TEXT    NOT NULL,
  attempt         INTEGER,
  created_at      DATETIME NOT NULL,
  consolidated_at DATETIME,
  UNIQUE(session_id, case_id, procedural_id)
);
CREATE INDEX idx_memory_usage_unconsolidated ON memory_usage(procedural_id) WHERE consolidated_at IS NULL;

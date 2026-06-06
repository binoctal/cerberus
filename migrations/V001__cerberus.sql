-- V001__cerberus.sql
-- Cerberus core schema. SQLite for MVP (C1-C3).
-- C4 will migrate to PostgreSQL for JSONB, pgvector, and GIN indexes.

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  mode TEXT NOT NULL CHECK (mode IN ('run', 'verify', 'serve')),
  status TEXT NOT NULL DEFAULT 'running',
  goal TEXT,
  project_name TEXT,
  coverage_pct REAL NOT NULL DEFAULT 0,
  stats TEXT NOT NULL DEFAULT '{}',
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name);

-- Traces
CREATE TABLE IF NOT EXISTS traces (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  category TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT,
  metadata TEXT
);
CREATE INDEX IF NOT EXISTS idx_traces_session ON traces(session_id);

-- Evidence
CREATE TABLE IF NOT EXISTS evidence (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  trace_id INTEGER NOT NULL REFERENCES traces(id),
  type TEXT NOT NULL,
  content TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_evidence_trace ON evidence(trace_id);

-- Verdicts
CREATE TABLE IF NOT EXISTS verdicts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  trace_id INTEGER NOT NULL REFERENCES traces(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pass', 'fail', 'uncertain', 'skip')),
  confidence REAL NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('judge', 'checker', 'merged')),
  reasoning TEXT,
  suggestions TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_verdicts_session ON verdicts(session_id);
CREATE INDEX IF NOT EXISTS idx_verdicts_status ON verdicts(status);

-- L1 Episodic Memory
CREATE TABLE IF NOT EXISTS memory_episodic (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  verdict TEXT,
  duration_ms INTEGER,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_episodic_target ON memory_episodic(target);
CREATE INDEX IF NOT EXISTS idx_episodic_created ON memory_episodic(created_at DESC);

-- L2 Semantic Memory (embedding + pgvector added in V002 for C4)
CREATE TABLE IF NOT EXISTS memory_semantic (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  tags TEXT NOT NULL DEFAULT '[]',
  confidence REAL NOT NULL DEFAULT 0.5,
  project_name TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- L3 Procedural Memory
CREATE TABLE IF NOT EXISTS memory_procedural (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  condition TEXT NOT NULL,
  action TEXT NOT NULL,
  effectiveness REAL NOT NULL DEFAULT 0.5,
  usage_count INTEGER NOT NULL DEFAULT 0,
  project_name TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Project Models
CREATE TABLE IF NOT EXISTS project_models (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_name TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  model TEXT NOT NULL,
  schema_version INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  UNIQUE(project_name, version)
);

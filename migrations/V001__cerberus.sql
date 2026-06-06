-- Cerberus core schema. No pgvector dependency (added in V002 for C4).

-- Sessions
CREATE TABLE IF NOT EXISTS sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  mode TEXT NOT NULL CHECK (mode IN ('run', 'verify', 'serve')),
  status TEXT NOT NULL DEFAULT 'running',
  goal TEXT,
  project_name TEXT,
  coverage_pct REAL NOT NULL DEFAULT 0,
  stats JSONB NOT NULL DEFAULT '{}',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name);

-- Traces
CREATE TABLE IF NOT EXISTS traces (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  category TEXT NOT NULL,
  target TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'running',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  metadata JSONB
);
CREATE INDEX IF NOT EXISTS idx_traces_session ON traces(session_id);

-- Evidence
CREATE TABLE IF NOT EXISTS evidence (
  id BIGSERIAL PRIMARY KEY,
  trace_id BIGINT NOT NULL REFERENCES traces(id),
  type TEXT NOT NULL,
  content JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_evidence_trace ON evidence(trace_id);

-- Verdicts
CREATE TABLE IF NOT EXISTS verdicts (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  trace_id BIGINT NOT NULL REFERENCES traces(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pass', 'fail', 'uncertain', 'skip')),
  confidence REAL NOT NULL,
  source TEXT NOT NULL CHECK (source IN ('judge', 'checker', 'merged')),
  reasoning TEXT,
  suggestions JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_verdicts_session ON verdicts(session_id);
CREATE INDEX IF NOT EXISTS idx_verdicts_status ON verdicts(status);

-- L1 Episodic Memory
CREATE TABLE IF NOT EXISTS memory_episodic (
  id BIGSERIAL PRIMARY KEY,
  session_id UUID NOT NULL REFERENCES sessions(id),
  target TEXT NOT NULL,
  status TEXT NOT NULL,
  verdict JSONB,
  duration INTERVAL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_episodic_target ON memory_episodic(target);
CREATE INDEX IF NOT EXISTS idx_episodic_created ON memory_episodic(created_at DESC);

-- L2 Semantic Memory (embedding column added in V002)
CREATE TABLE IF NOT EXISTS memory_semantic (
  id BIGSERIAL PRIMARY KEY,
  content TEXT NOT NULL,
  source TEXT NOT NULL,
  tags TEXT[] NOT NULL DEFAULT '{}',
  confidence REAL NOT NULL DEFAULT 0.5,
  project_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_semantic_tags ON memory_semantic USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_semantic_search ON memory_semantic USING GIN(to_tsvector('english', content));

-- L3 Procedural Memory
CREATE TABLE IF NOT EXISTS memory_procedural (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  condition TEXT NOT NULL,
  action TEXT NOT NULL,
  effectiveness REAL NOT NULL DEFAULT 0.5,
  usage_count INT NOT NULL DEFAULT 0,
  project_name TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Project Models
CREATE TABLE IF NOT EXISTS project_models (
  id BIGSERIAL PRIMARY KEY,
  project_name TEXT NOT NULL,
  version INT NOT NULL DEFAULT 1,
  model TEXT NOT NULL,
  schema_version INT NOT NULL DEFAULT 1,
  source TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(project_name, version)
);

-- V003__session_plans.sql
-- Persist test plans for session resumption.

CREATE TABLE IF NOT EXISTS session_plans (
  session_id TEXT PRIMARY KEY REFERENCES sessions(id),
  plan_json TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

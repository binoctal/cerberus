-- Persist the per-case identity of a verdict row. Target is NOT a case key
-- (deterministic generators emit many cases sharing one service target), so
-- store-only consumers (findings pull) need the case id to map a failing
-- verdict back to its plan case. Empty string = legacy row predating the
-- column (persists from 2026-08-16 on).
ALTER TABLE verdicts ADD COLUMN case_id TEXT NOT NULL DEFAULT '';

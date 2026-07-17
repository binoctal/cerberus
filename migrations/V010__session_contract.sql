-- Add contract column to sessions for persisting the coverage contract so the
-- Examiner can assess on resume (Scout phase, which builds the contract, is
-- skipped during resume).
ALTER TABLE sessions ADD COLUMN contract TEXT NOT NULL DEFAULT '';

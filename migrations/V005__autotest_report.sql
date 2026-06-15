-- Add autotest_report column to sessions for persisting AutoTest phase results.
ALTER TABLE sessions ADD COLUMN autotest_report TEXT NOT NULL DEFAULT '';

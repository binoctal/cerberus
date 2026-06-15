-- Add failure_reason column to verdicts for detailed failure classification.
-- This helps distinguish between system bugs and LLM/environment issues.
ALTER TABLE verdicts ADD COLUMN failure_reason TEXT NOT NULL DEFAULT '';

-- Mark a verdict row as a recovered fallback (A1 Phase 2 follow-up). A recovered
-- verdict's status stays the Examiner's judgment (pass); this column is the
-- orthogonal signal that the role was rescued by a lazy fallback, so downstream
-- tallies and reports can treat it as a distinct outcome.
ALTER TABLE verdicts ADD COLUMN recovered INTEGER NOT NULL DEFAULT 0;

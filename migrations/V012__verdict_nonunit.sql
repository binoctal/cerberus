-- Persist the non-unit identity of a verdict row, mirroring the Agent-side
-- TestCase.FallbackFor / TestCase.Replaces signals. A committed verdict can
-- already be flagged Recovered (V011); these two columns let the consolidate
-- committed-verdict loop identify an UNRECOVERED non-unit (a fallback or
-- replacement that also failed) and skip it, so it cannot shadow its primary's
-- failure reason in the effectiveness map. Empty string = independent unit.
ALTER TABLE verdicts ADD COLUMN fallback_for TEXT NOT NULL DEFAULT '';
ALTER TABLE verdicts ADD COLUMN replaces TEXT NOT NULL DEFAULT '';

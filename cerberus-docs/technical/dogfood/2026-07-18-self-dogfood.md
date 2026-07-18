# Cerberus Self-Dogfood — 2026-07-18

Ran cerberus against its own source (`internal/detect`) using the real GLM provider (via `.claude/settings.json`). Goal: verify the product actually works end-to-end on its own code, not just under mock tests.

## Setup

- Built `./build/cerberus`, ran `cerberus run --config .cerberus/runtime/self-dogfood.yaml --dir . --goal "..."` (local mode, no services, 60k token budget, 8m cap).
- LLM credentials/model/base URL inherited from `.claude/settings.json` (GLM via `ANTHROPIC_AUTH_TOKEN` + `ANTHROPIC_BASE_URL`).

## Finding 1 (P0, fixed): V009 seed migration bricked `cerberus run`

The first run died before any test executed:

```
run migrations: apply migration V009__seed_regression_data.sql:
constraint failed: UNIQUE constraint failed: bug_tracker.bug_id (2067)
```

Root cause: `V009` used plain `INSERT` with a hardcoded `bug_id 'BUG-001'`. The persistent DB already had a runtime `bug_tracker` row with that id (written by `CreateBugRecord` during normal operation), so re-running migrations after a cerberus upgrade failed. Because the migration runs inside a transaction, `schema_migrations` never recorded version 9, so every subsequent `cerberus run` retried V009 and failed the same way — **permanently bricked, no automatic recovery**.

Fix: all 9 seed `INSERT`s → `INSERT OR IGNORE INTO` (`4f97620`). Safe to edit in place: V009 had never recorded in any DB.

Lesson: **every seed/data migration must be idempotent** (`INSERT OR IGNORE` or `ON CONFLICT`). The transaction guarantees schema-version atomicity, but not protection against data that the running system legitimately produces with the same key.

## Finding 2 (minor, unfixed): LLM action missing `target_path` → fallback

```
action unusable, using fallback  error="target_path is required"
```

One LLM-emitted action lacked `target_path`; the parse-fallback path recovered it. Not a failure, but the model produced a malformed action that the engine had to paper over. Worth a look at whether the prompt underspecifies required action fields.

## Finding 3 (minor, unfixed): verdict vs summary disagreement on `uncertain`

Verdict log showed `tc-001` and `tc-002` as `uncertain` (correctness 0.4 / 0.3). The session summary reported `7 pass, 0 fail, 1 skip, 0 uncertain` with `Pending review: 2`. The two `uncertain` verdicts appear to have been counted as `pass` in the summary while also surfacing as pending review. Either the summary's `pass` bucket includes uncertain, or the `uncertain` counter is wrong. Cosmetic, but confusing.

## Outcome

After the migration fix, the same run completed cleanly:

```
Verdicts: 7 pass, 0 fail, 1 skip   |   ~7K tokens   |   52s
```

Agent exercised `file_exists`, `file_glob`, `process_exec` (`go test`, `go vet`, `go build ./...`, `go test ./...`) — read-only + command execution, no source files written (auto-test-safety off). Rule engine hit rate 43% (3 hits / 4 misses). Reflexion stored 6 reflections.

## Confirmed

- cerberus starts, migrates, plans (8 cases), executes (ReAct steer loop), judges (Examiner), and persists — end-to-end on a real LLM against its own code.
- The migration brick was a latent production bug that mock tests could not catch (they use `:memory:` DBs that never accumulate runtime `bug_tracker` rows). This is the case for periodic self-dogfood against a long-lived DB.

# AutoTest: Coverage Gap Detection + Test Generation

`cerberus run` runs an **AutoTest phase** after Examiner: it executes the
project's Go tests with coverage, finds uncovered code, AI-generates `_test.go`
files, and verifies them. Generated tests that do not pass or do not raise
coverage are reverted automatically.

## Trigger

Disabled by default. Enable per-run:

```bash
cerberus run --goal "..." --auto-test-safety=dry-run   # report only
cerberus run --goal "..." --auto-test-safety=approve   # prompt before each write
cerberus run --goal "..." --auto-test-safety=auto      # write directly, report after
```

## Safety modes

| mode | behavior |
|---|---|
| `off` (default) | AutoTest phase skipped entirely |
| `dry-run` | report uncovered gaps + generated test content; write nothing |
| `approve` | EscalationGate prompts before each `_test.go` is written |
| `auto` | write directly; report a git-style diff after |

## Guarantees

- Never leaves a test that breaks `go test`. A generated test is kept only if it
  passes **and** strictly raises coverage; otherwise it is reverted.
- Does not run on a failing baseline: if existing tests fail, AutoTest aborts.

## What it does

1. Runs `go test -coverprofile` on the project.
2. Parses the profile to find uncovered functions (0% coverage) and source files
   with no `_test.go`.
3. For each gap, reads the function source (`go/parser`) and asks the LLM to
   generate a table-driven `_test.go`.
4. Writes the test (subject to the safety mode), re-runs `go test`, and reverts
   unless the new test passes and coverage strictly rises.

## Exclusions (YAGNI)

Skips: cgo, generated code (`.pb.go`/`_gen.go`), `main` packages, `vendor`,
`node_modules`, `_test.go` files themselves. No fuzz/benchmark generation.

## Language support

Go today (`go test -coverprofile` + `go/parser`). Node (`jest --coverage`) and
Python (`pytest --cov`) arrive later under the same interfaces.
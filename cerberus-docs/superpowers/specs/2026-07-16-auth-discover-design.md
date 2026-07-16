# Auth Discover Command — Design

Date: 2026-07-16
Status: Implemented — Component 3a, merged to main 2026-07-16
Builds on: `2026-07-12-auth-flow-design.md` (Components 1, 2, 4 are implemented and merged to main).

## Problem

The declarative `auth:` block added by the auth-flow work lets cerberus obtain
a dynamic credential at run time — but authoring that block by hand is hard.
Getting it right requires reading the target service's source: locating the
login endpoint, its request-body shape (field names like `email` vs `username`),
and the JSON path of the returned token (`token` vs `data.accessToken`). A user
guessing these fields produces a flow that logs in but fails to extract the
token, and the failure mode (200 with a missing-field degrade) is not obvious.

## Goal

A `cerberus auth discover` command that reads the target service's source code,
asks the LLM to infer an `AuthFlow`, and writes the resulting `auth:` block into
`project.yaml` after explicit user confirmation. It is a one-shot authoring aid:
the command writes disk (with confirmation); it does not run at session time.
The Scout runtime fallback (spec Component 3b) is out of scope here and will be
a separate plan.

## Decisions (locked)

- **Scope: command only (3a).** The Scout runtime fallback (3b) is deferred —
  it depends on this command's inference logic but is a separate subsystem.
- **Data source: read target source code.** The LLM is fed relevant source
  snippets from `Code.Root`, not just the `ProjectModel` endpoint list, because
  body field names and the token response path are only visible in source.
- **YAML write-back: whole-file rewrite.** Follow the existing `cerberus
  discover` precedent (`main_discover.go`: `LoadFromFile` → mutate →
  `yaml.Marshal` → `WriteFile`). Simpler than `yaml.Node` surgical insert; the
  project already accepts whole-file rewrite for `discover`.
- **Architecture: standalone `internal/authdiscover/` package.** Owns file
  selection + one LLM call; the command is a thin caller. Independent of the
  Scout session lifecycle and independently testable.
- **Structured output: `Driver.Decide(prompt, schema)`.** The `ai.Driver`
  already deserializes the LLM's JSON output into the provided schema struct
  (with retry/cache); the command passes `&discoverOutput{}`. Not native
  tool-use / response_schema.
- **Driver construction: direct public API.** `auth discover` builds its single
  driver inline in `cmd/` from existing public primitives —
  `llm.NewClientWithConfig` (model from `projCfg.Settings.AIBudget.Model`;
  API key / base URL / auth scheme from the global `config.Load()` result, the
  same fields `main_run.go` reads: `LLMAPIKey`, `LLMBaseURL`, `LLMAuthScheme`)
  then `ai.NewDriver`. `SetupHeadDrivers` is left untouched: its complexity is
  per-head model picking (`PickModel`/`tiers`), which the command does not
  need, so there is nothing worth extracting.
- **Validation reuse: export it.** `validateAuthFlow` is unexported and coupled
  to `(actorIdx, Actor) string`; export the core check as
  `project.ValidateAuthFlow(*AuthFlow) error` so `authdiscover` rejects invalid
  suggestions without duplicating the rules.
- **Token lifecycle: unchanged.** This command only *authors* the block; the
  one-shot login-at-setup behavior from Component 2 is reused verbatim.

## Command

```
cerberus auth discover --actor <name> [--service <name>] [--dry-run]
```

- `--actor` (required): the actor whose `auth:` block is written. Must exist in
  `project.yaml`; error if not found.
- `--service` (optional): the service whose source is read and whose URL is the
  `login.path` base. Defaults to `actor.Service`, else the first configured
  service. Error if it resolves to no URL and `login.path` cannot be made
  absolute. Resolution mirrors `Session.serviceURLForActor` (Component 2);
  extract that small lookup to a shared spot or replicate it in `cmd/` (it is a
  `Session` method today, unavailable to the command).
- `--dry-run`: print the suggested `auth:` YAML only; do not prompt, do not
  write.

### UX flow

1. Load `.cerberus/project.yaml`; locate the target actor; resolve the service.
   Note whether the actor already has an `auth:` block (drives the prompt in 7).
2. Read and select source files from `Code.Root` (see Package below).
3. Call the LLM once; deserialize into `discoverOutput`.
4. If `Found` is false, print "no login endpoint found in <service>" and exit
   without writing.
5. Map the output to `*project.AuthFlow`; validate with the exported
   `project.ValidateAuthFlow` (required fields + interpolation vars +
   `inject_as` colon — see Validation below); reject if invalid.
6. Print the suggested `auth:` block as YAML.
7. Unless `--dry-run`: prompt `Write to <actor> in project.yaml? [y/N]` — or
   `actor <name> already has auth, overwrite? [y/N]` when a block exists; on
   `y`, write back (whole-file rewrite); otherwise exit without writing.

## Package `internal/authdiscover/`

Core entry point:

```go
// Discover reads the target service's source, asks the LLM to infer an
// AuthFlow, and returns it (not yet written to disk). The driver is passed in
// (the command builds it via the shared constructor; tests inject a mock) so
// no LLM-client construction happens inside. The command layer owns
// confirmation and file mutation.
func Discover(ctx context.Context, driver *ai.Driver, cfg *project.Config, actorName, serviceURL string) (*project.AuthFlow, error)
```

### File selection

- Walk `cfg.Code.Root` with `filepath.WalkDir`. `internal/architecture`'s walk
  is NOT reusable — it is an `*Analyzer` method bound to `projectPath` and
  hard-codes `.go` only; authdiscover needs multi-language source. So it owns a
  small walk that skips `vendor/`, `node_modules/`, `build/`, `dist/`, `.git/`
  and keeps files matching supported extensions (`.go`, `.ts`, `.tsx`, `.js`,
  `.jsx`, `.py`).
- Score each surviving file by keyword hits (`login`, `signin`, `sign-in`,
  `auth`, `session`, `jwt`, `token`, `bearer`, `middleware`, `route`,
  `passport`, `handler`).
- Take top-N by score, capped by a total-byte budget so the prompt fits the
  model window. N and the budget are package constants, chosen for a typical
  login flow (a handful of files). The selected source snippets already
  contain the route definitions, so no separate routing hint is needed (the
  command is standalone and does not run the full ProjectModel analysis).

### LLM call

- The driver is built by the command (see Driver construction above) and passed
  in as the `driver` arg — `Discover` itself never constructs clients, so tests
  inject a mock `*ai.Driver`.
- Prompt: describe the task (infer a single login flow), include the selected
  source snippets, include the actor's credential **field names** (`email`,
  `password`) so the model emits `{email}`/`{password}` placeholders — never
  include credential **values**. The prompt MUST also inline the exact JSON
  shape to return (field names/types/meaning), because `Driver.Decide` does not
  inject the schema into the prompt — it only parses the response.
- Invoke `driver.Decide(ctx, prompt, &discoverOutput{})`; the Driver unmarshals
  the JSON response into the struct (retry/cache handled there). `Decide`'s
  parse-failure error embeds the raw LLM response, so `Discover` wraps it and
  surfaces only "could not parse LLM output into AuthFlow" (see Error handling
  / security). Any error → write nothing.

### Structured output

```go
type discoverOutput struct {
    // Found is the model's assertion that a login flow exists. false → the
    // command reports "no login endpoint found" and writes nothing, rather
    // than fabricating an AuthFlow for a public API.
    Found bool `json:"found"`
    Login struct {
        Method  string            `json:"method"`
        Path    string            `json:"path"`
        Body    map[string]string `json:"body"`
        Headers map[string]string `json:"headers"`
    } `json:"login"`
    TokenFrom string `json:"token_from"`
    InjectAs  string `json:"inject_as"`
    // Notes is a free-text rationale the model returns for the user; never
    // written to project.yaml, only printed.
    Notes string `json:"notes"`
}
```

The `Login` / `TokenFrom` / `InjectAs` fields map onto `project.AuthFlow` /
`project.AuthLogin` (`Found` / `Notes` do not). On JSON parse error, empty
required fields, or an `inject_as` without a colon, `Discover` returns a
wrapped error that does not expose the raw LLM response (see Error handling /
security); the command surfaces it and writes nothing.

## Validation

`validateAuthFlow` (in `internal/project/validate_auth.go`) is currently
unexported and coupled to `(actorIdx int, a Actor) string`. Export the core
check as `project.ValidateAuthFlow(af *AuthFlow) error` and have
`validateAuthFlow` delegate to it. `Discover` calls the exported form to reject
invalid suggestions — missing required field, an interpolation variable with no
matching credential, or an `inject_as` without a colon — before they reach
disk. The colon check reuses the same logic as `agent.splitHeader`.

## Write-back (command layer)

Mirror `main_discover.go`:

1. `project.LoadFromFile(cfgPath)` → `*project.Config`.
2. Find the actor by name; set `cfg.Actors[i].Auth = discovered`.
3. `out, err := yaml.Marshal(cfg)`.
4. `os.WriteFile(cfgPath, out, 0644)` (after confirmation).

## Error handling / security

- Any failure (LLM error, JSON invalid, validation failure, `inject_as`
  malformed) → command prints the error and exits non-zero; `project.yaml` is
  not modified.
- `Driver.Decide`'s parse-failure error embeds the raw LLM response, which may
  echo source snippets or secrets the model copied. `Discover` wraps that error
  and exposes only "could not parse LLM output into AuthFlow"; the raw response
  stays in debug logs, never the user-facing error or stdout.
- Credential **values** are never placed in the prompt, the model output echo,
  or logs. Only credential field names are used as placeholder hints.
- The suggested block is printed in full before the write prompt so the user
  can review exactly what lands in version control.
- `Code.Root` walk is bounded by the file-count / byte budget; a missing or
  empty `Code.Root` produces a clear error rather than an unbounded scan.

## Testing

- `internal/authdiscover/`:
  - `Discover` with a mock `*ai.Driver` (built from `llm.NewMockClient` via
    `ai.NewDriver`) returning valid `discoverOutput` JSON, injected as the
    `driver` arg → asserts the mapped `*project.AuthFlow` fields.
  - Mock returning invalid JSON / missing fields → asserts a non-nil error and
    nil result.
  - File selection: a tempdir with seeded source files of varying keyword
    relevance → asserts the top-N selection picks the high-relevance files and
    respects the byte budget; vendored/build dirs are excluded and multiple
    languages (`.go`, `.ts`, `.py`) are admitted.
  - `Found: false` → the result signals "no login flow" distinctly from a hard
    error, so the command exits cleanly without writing.
  - Parse failure: a mock response containing a known marker string → the
    returned error does NOT contain that marker (raw LLM response is not
    leaked through `Decide`'s error).
  - Prompt construction: the built prompt contains the inlined JSON shape
    (`found`, `login`, `token_from`, `inject_as`) and credential field names,
    but no credential values.
- CLI (`cmd/cerberus`):
  - `--dry-run` prints the block and does not touch `project.yaml`.
  - Confirmed write: tempdir `project.yaml` + mock → asserts the target actor's
    `Auth` is set and other actors are unchanged. When the actor already has an
    `auth:` block, the overwrite prompt is used and only proceeds on
    confirmation.
  - Unknown actor / unknown service → non-zero exit, no write.
- `make check` (fmt + lint + race) green.

## Acceptance

Against a target with a real login endpoint (e.g. open-agents
`/api/dev/setup`): run `cerberus auth discover --actor test-user`, confirm the
suggested block matches the actual login path / body / token field, accept it,
then run a session — the Agent authenticates via the now-permanent block, with
no manual source reading by the user.

## Out of scope (future)

- Spec Component 3b — Scout runtime fallback: in-memory discovery during
  planning when an actor has no `auth:` block or login failed. Reuses this
  package's inference logic. Separate plan.
- Cookie / multi-step OAuth flows.
- Re-discovering / refreshing an existing `auth:` block (currently overwrites
  on confirm).

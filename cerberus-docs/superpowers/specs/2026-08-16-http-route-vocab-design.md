# HTTP Route Vocabulary Extraction & Coverage Attribution — Design

Date: 2026-08-16
Status: approved (brainstorm session 2026-08-16)
Closes: fidelity-ladder Task 8 item 2 (`superpowers/plans/2026-08-14-fidelity-ladder-real-e2e.md`)

## Problem

`vocabextract` extracts only WS routing edges (DO `broadcastToWeb`/`sendToBridge`
anchors in `room.ts`). The HTTP surface is invisible to the vocabulary and to
path coverage except for hand-declared `http_triggers` (HTTP→WS pushes).
Coverage audits that "start from the product promise" cannot see plain REST
routes (`/api/sessions`, `/api/missions`, …) — the gap is silent.

## Decisions (approved in-session)

1. **Denominator: ALL mounted routes.** Every extracted route enters the
   path-coverage denominator. Exemptions flow through the existing
   `partial`/`unsupported` marks (preserved on re-extraction) — not through
   silent filtering. Fidelity over cosmetics.
2. **v1 scope: extraction + attribution only.** No deterministic HTTP case
   generation. Gap output names unexercised routes; generation strategy is a
   later decision informed by v1's gap data (auth-shape distribution).
3. **Model: separate `http_routes` list.** HTTP routes do not masquerade as
   `VocabEdge`s on disk. Coverage synthesizes one `VocabEdge` per route in
   `requiredEdges()` — the same pattern `http_triggers` already uses.

## 1. Data model (`internal/project/vocabulary.go`)

```go
type Vocabulary struct {
    Source     VocabSource       `yaml:"source" json:"source"`
    Edges      []VocabEdge       `yaml:"edges" json:"edges"`
    HTTPRoutes []VocabHTTPRoute  `yaml:"http_routes,omitempty" json:"http_routes,omitempty"`
}

type VocabHTTPRoute struct {
    Method      string          `yaml:"method" json:"method"`
    Path        string          `yaml:"path" json:"path"`
    Mount       string          `yaml:"mount,omitempty" json:"mount,omitempty"`
    Partial     bool            `yaml:"partial,omitempty" json:"partial,omitempty"`
    Unsupported bool            `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
    Source      VocabEdgeSource `yaml:"source" json:"source"`
}
```

- Identity key: `METHOD|path` (normalized full path).
- `Method` ∈ {GET, POST, PUT, DELETE, PATCH, OPTIONS, HEAD, ALL}.
- `Path` is the full normalized pattern: mount chain + route path
  (`/api` + `/sessions` + `/:id` → `/api/sessions/:id`). Join rule: no double
  slashes; sub-app root `'/'` contributes nothing.
- `ALL` matches any method at attribution time (Hono `app.all`).
- `*` suffix wildcard matches one-or-more remaining segments
  (Hono `/api/workflows/jobs/*`).
- Marks (`partial`/`unsupported`) have the same lifecycle as edge marks:
    live-probe knowledge, preserved across re-extraction, excluded from the
    denominator while set.

## 2. Extractor (`internal/vocabextract/extractor.mjs`)

New HTTP pass alongside the existing WS pass:

- Entry: the `--from` file (e.g. `worker.ts`). The WS pass runs only when the
  file contains a class (DO room); the HTTP pass runs at module level. Both
  passes coexist; either may be empty.
- HTTP pass finds `X.get/post/put/delete/patch/options/head/all('path', …)`
  call expressions and `X.route('/prefix', routerVar)` mounts, where `X` is
  any `new Hono()`-typed identifier (heuristic: variable initialized from
  `new Hono(...)` or the file's default export pattern used by `worker.ts`).
- Import resolution replicates Node semantics: for specifier `./routes/auth`,
  try `<dir>/routes/auth` (exact file), then `+.ts`, then `+/index.ts` — file
  beats directory, matching how `worker.ts` imports resolve (known-issues
  note items 2–3 depend on this). Depth cap 8; visited-set prevents cycles.
  Only relative specifiers are followed.
- Each route emits `{method, path, mount, source: {spans}}`. `app.on('GET', p)`
  multi-method registrations are counted in a `skipped_on_registrations`
  output field (visibility) but not extracted (best-effort boundary, logged
  not silent).
- Duplicate registration of the same `METHOD|path` (middleware variants)
  merges spans.
- Output JSON gains top-level `http_routes` and `files`
  (`[{path}]` — entry plus every traversed source file, absolute). The WS
  pass output shape is unchanged; `files` covers both passes (entry file
  always listed).

## 3. CLI (`cmd/cerberus/main_protocol.go`)

- `runProtocolVocabulary` unmarshals `edges` AND `http_routes`; hashes every
  file reported by `files` into `VocabSource.Files`.
- Re-extraction preserves `partial`/`unsupported` on routes keyed
  `method|path`, mirroring the WS edge marks-merge.
- Command `Short` updated: "Extract a WS routing vocabulary and HTTP route
  surface from a TypeScript source file".

## 4. Coverage attribution (`internal/session/coverage.go`)

- `requiredEdges()`: for each non-partial/unsupported `HTTPRoute`, synthesize
  `VocabEdge{FromRole: "http_client", ToRole: "api", Type: "METHOD path",
  Trigger: "http_request"}`.
- `exercisedEdges()`: new branch — evidence with `Action == "http_request"`
  and non-empty `Method`/`URL` credits every required route whose pattern
  matches. ANY response status credits (4xx/5xx prove the route was reached;
  auth semantics belong to the violations layer). URL query strings are
  ignored; matching is on the path portion.
- `Evidence` (`internal/head/agent/types.go`) gains `Method`, `URL`,
  `StatusCode` fields (json omitempty), populated in `stepEvidence` for
  `http_request` steps from `TestStep.Method` (default GET) and
  `types.HTTPResult`. Rule-engine HTTP evidence carries no URL and is NOT
  attributed in v1 — documented boundary.
- `sessionHasVocab()` returns true when any service declares WS edges OR
  `http_routes`. Side effect (intended fix): a protocol with `http_triggers`
  but no WS edges now routes to path coverage instead of silently falling
  back to line coverage.
- Gap output: `edge http_client→api POST /api/sessions/:id not exercised`
  via the existing `Kind="path"` informational channel.

## 5. Validation (`internal/project`)

`ValidateVocabulary` (new, called from `LoadVocabulary` and the project
loader): method enum, path starts with `/` and has no `//`, `:param` and `*`
segments well-formed (`*` only as final segment). A broken denominator must
not pass silently — same principle as claims.

## 6. Testing

- Extractor (node-spawning tests, existing skip pattern):
  `testdata/hono/worker.ts` fixture — entry inline route, two-level mount
  (`routes/things.ts`, `routes/nested/index.ts`), `:param` route, `*`
  wildcard, duplicate registration (span merge), `app.on` (skipped+counted),
  unmounted import (excluded).
- Attribution unit tests: matcher (`:param`, `*`, query ignored, `ALL`
  matches any method, 401 credits), denominator synthesis (partial/unsupported
  excluded), `sessionHasVocab` fix regression, marks-merge preserve.
- Validation unit tests: bad method, `//` in path, mid-path `*`.

## Out of scope (v1)

- Deterministic HTTP case generation (deferred; informed by v1 gap data).
- Auth-requirement inference per route (JWT vs device-token vs public).
- `app.on(method, path)` extraction (counted, not extracted).
- Rule-engine HTTP evidence attribution.

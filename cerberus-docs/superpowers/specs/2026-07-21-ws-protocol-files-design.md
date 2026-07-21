# WebSocket Realtime Engine (M3-1) — Standalone Protocol Files (Design)

**Date:** 2026-07-21
**Status:** Design (brainstormed; pending spec review)
**Scope:** `internal/project/` (`Service.ProtocolRef` schema; `LoadFromYAML` dir-aware loading + protocol-file resolution), `cerberus-docs/` (config reference + executor doc note)
**Depends on:** M1 (`protocol:` block), M2 (roles/handshake/assertions — all already fields on `Protocol`)
**Proposal:** `cerberus-docs/superpowers/specs/2026-07-20-ws-realtime-engine-m3-proposal.md`

## Background & Motivation

M1 let a human declare a service's WS protocol facts in an **inline** `protocol:`
block on the Service. M2 added roles/handshake to that block. The `Protocol`
struct (`internal/project/protocol_schema.go`) is now complete: framing,
type_path, auth, roles. But the declaration is **per-service, inline in
`project.yaml`** — it cannot be reused across services or shared across projects.
Two deployments speaking the same protocol re-declare identical facts.

M3 (per its proposal) promotes the description to a **first-class, shareable
artifact**: a standalone YAML file under `.cerberus/protocols/<name>.yaml`,
referenced from `project.yaml` by name. This sub-project (M3-1) delivers the
artifact layer — the file format and reference resolution — which M3's later
items (Scout-generated cases, auto-inference) will consume and produce.

The proposal gates M3 on dogfooding signals; this sub-project is the one piece
that is **not** signal-dependent: the file format is fully determined by the
existing `Protocol` struct, and reference resolution is deterministic config
mechanics. The reuse/ergonomics value is realized later; the mechanics are
stable now.

## Goal

A service may declare its protocol **either** inline (`protocol: { ... }`, M1,
unchanged) **or** by reference (`protocol_ref: <name>`), where the named file
`.cerberus/protocols/<name>.yaml` is loaded at config-load time and attached as
the service's `Protocol`. The resolved protocol then behaves exactly like an
inline one (M1/M2 executor behavior, validation, secret hygiene — all
unchanged).

Success criteria:

- `protocol_ref: <name>` on a service loads `<baseDir>/.cerberus/protocols/<name>.yaml`,
  parses it into a `Protocol`, and attaches it (equivalent to inlining).
- Inline `protocol:` and `protocol_ref:` are mutually exclusive; setting both is
  a load error.
- A missing or unparseable referenced file is a clear load error naming the
  service and the ref.
- Path traversal is rejected: a `protocol_ref` containing `/`, `\`, or `..` is a
  load error (the ref must be a plain name).
- An inline `protocol:` service behaves exactly as before (byte-identical
  fallback).
- The resolved protocol flows through existing validation (`ValidateProtocol`)
  and the WS executor (`BuildWSProtocolIndex`) unchanged — no executor edit.
- `make check` green; table-driven tests in `internal/project/`.

## Non-Goals

- **A protocol registry / marketplace.** Files are shared manually via git until
  a registry is proven needed (proposal Open Question 1).
- **Scout-generated cases / auto-inference.** Those are later M3 sub-projects;
  this sub-project only delivers the file format + reference resolution.
- **Cross-file references / protocol inheritance / composition.** A protocol
  file is a leaf `Protocol` value; no `$ref`, no extends, no nesting. YAGNI.
- **Changing the inline `protocol:` form.** It remains fully supported
  (backward compatible). The standalone file is an alternative, not a
  replacement (proposal D1: inline is a "transitional stepping stone"; removal
  is not in scope).
- **Capture-based inference** (record a session, infer a description) — future
  (proposal D3 (b)).

## Design Decisions

### D1 — A new `protocol_ref` field (not overloading `protocol:`)

`Service` gains `ProtocolRef string \`yaml:"protocol_ref,omitempty"\``. A service
sets `protocol_ref: <name>` to reference a file, OR `protocol: { ... }` to
inline — never both.

**Rejected — overloading `protocol:` to accept either a string (ref) or a map
(inline).** That needs a custom `UnmarshalYAML` on `Protocol` (a string-or-struct
union), which is more code and more error surface for no gain. A separate field
is YAML-native, backward compatible, and unambiguous (the mutually-exclusive
check is a one-liner).

### D2 — File location is conventional: `<baseDir>/.cerberus/protocols/<name>.yaml`

The ref names a file under the project's `.cerberus/protocols/` directory
(matching the proposal verbatim and the existing `.cerberus/` config convention
in CLAUDE.md). `baseDir` is the directory containing `project.yaml` (i.e.
`filepath.Dir(cfgPath)`, already known to `LoadFromFile`).

The file is a direct YAML serialization of the `Protocol` struct — the same keys
M1/M2 use inline (`framing`, `type_path`, `auth`, `roles`). Example
`.cerberus/protocols/open-agents.yaml`:

```yaml
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web:
    credential_ref: web-actor
    params: { type: web }
    handshake: { await_type: devices:sync, timeout: 5 }
  bridge:
    credential_ref: bridge-actor
    params: { type: bridge }
```

The file is **version-controlled** (sibling of `project.yaml`, not under the
gitignored `.cerberus/runtime/`), so it is the shareable artifact.

### D3 — Resolution happens at load time, before Validate; `LoadFromYAML` becomes dir-aware

`Validate` is a pure check (no file I/O — confirmed). Protocol-file resolution
is a load-time concern, so it runs **before** `Validate`, at the point the
config bytes + the project directory are both in hand.

`LoadFromYAML(data []byte)` today takes only bytes and internally calls
`Validate`. To resolve `protocol_ref` it needs the project directory. Decision:
**`LoadFromYAML` gains a `baseDir string` parameter.** `LoadFromFile` passes
`filepath.Dir(path)`; byte-only callers (tests) pass `""` (a `protocol_ref` with
`baseDir == ""` is a load error — bytes-only load cannot read files). This is an
internal package, so the signature change ripples to exactly three callers
(`loader.go` internal, `schema_test.go`, `smoke_test.go`), each a one-token
(`""`) addition when they use no `protocol_ref`.

`resolveProtocolRefs(cfg, baseDir)` runs after `applyDefaults`, before
`Validate`:

```go
func resolveProtocolRefs(cfg *Config, baseDir string) error {
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		if svc.ProtocolRef == "" {
			continue
		}
		if svc.Protocol != nil {
			return fmt.Errorf("services[%d]: protocol and protocol_ref are mutually exclusive", i)
		}
		if baseDir == "" {
			return fmt.Errorf("services[%d]: protocol_ref %q requires loading from a project directory", i, svc.ProtocolRef)
		}
		if err := checkProtocolRefName(svc.ProtocolRef); err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: %w", i, svc.ProtocolRef, err)
		}
		path := filepath.Join(baseDir, ".cerberus", "protocols", svc.ProtocolRef+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: %w", i, svc.ProtocolRef, err)
		}
		var p Protocol
		if err := yaml.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: parse: %w", i, svc.ProtocolRef, err)
		}
		svc.Protocol = &p
		svc.ProtocolRef = "" // resolved; clear so env-overlay re-resolution is a no-op
	}
	return nil
}
```

`checkProtocolRefName` rejects a ref containing `/`, `\`, or `..` (path
traversal defense; the ref must be a plain name). After a successful resolution,
`ProtocolRef` is cleared so the function is **idempotent**: the env-overlay path
in `LoadFromFile` re-runs `resolveProtocolRefs` + `Validate` after merging, and
already-resolved services (ProtocolRef cleared) are skipped while any
overlay-introduced refs are resolved.

### D4 — Env overlay re-resolves (idempotent)

`LoadFromFile`'s optional env-overlay (`CERBERUS_ENV`) merges another config and
re-validates. After the merge + `applyDefaults`, it also calls
`resolveProtocolRefs(cfg, dir)` before re-`Validate`, so a `protocol_ref`
introduced by the overlay is resolved too. Because D3 clears `ProtocolRef` after
resolution, base-config refs (already resolved by the initial `LoadFromYAML`)
are skipped on the second pass.

### D5 — No executor / validation-logic change

Once `svc.Protocol` is populated (inline OR resolved-from-file), everything
downstream is unchanged: `ValidateProtocol` validates it (Phase 6), and
`BuildWSProtocolIndex` feeds it to the WS executor. No `internal/head/agent/`
edit. This is purely a `internal/project/` config-loading feature.

## Schema & Loader Changes

**`internal/project/schema.go`** — `Service` gains:

```go
// ProtocolRef optionally names a standalone protocol description file
// (.cerberus/protocols/<name>.yaml) to load as this service's Protocol.
// Mutually exclusive with Protocol (inline). Empty means use Protocol (or none).
ProtocolRef string `yaml:"protocol_ref,omitempty"`
```

**`internal/project/loader.go`** —

- `LoadFromYAML(data []byte, baseDir string) (*Config, error)` — add `baseDir`;
  insert `resolveProtocolRefs(&cfg, baseDir)` between `applyDefaults(&cfg)` and
  `cfg.Validate()`.
- `LoadFromFile(path string)` — pass `filepath.Dir(path)` to `LoadFromYAML`;
  in the env-overlay branch, call `resolveProtocolRefs(cfg, dir)` before the
  re-`Validate`.
- New `resolveProtocolRefs(cfg *Config, baseDir string) error` and
  `checkProtocolRefName(name string) error` (package-private).
- New imports in loader.go: `path/filepath` (already present), `os` (already
  present), `gopkg.in/yaml.v3` (already present). No new deps.

**Caller updates** (signature ripple): `internal/project/schema_test.go:86` and
`internal/smoke/smoke_test.go:116` add `, ""` to their `LoadFromYAML` calls.

**Unchanged:** `validate_protocol.go`, the executor, `BuildWSProtocolIndex`,
the inline `protocol:` path, secret hygiene.

## Testing Strategy

Table-driven in `internal/project/loader_test.go` (and one schema round-trip).

- **ref resolves:** write `.cerberus/protocols/p1.yaml` with a Protocol; a
  service `protocol_ref: p1` loads it; `cfg.Services[0].Protocol` equals the
  file's content (framing/type_path/auth/roles). The resolved protocol passes
  `ValidateProtocol`.
- **inline unchanged:** a service with inline `protocol:` behaves exactly as
  before (regression).
- **mutually exclusive:** a service with both `protocol:` and `protocol_ref:` →
  load error mentioning "mutually exclusive".
- **missing file:** `protocol_ref: ghost` with no file → load error mentioning
  the ref.
- **unparseable file:** a `.cerberus/protocols/bad.yaml` with invalid YAML →
  load error mentioning "parse".
- **path traversal rejected:** `protocol_ref: ../../etc/passwd` and
  `protocol_ref: a/b` → load error (checkProtocolRefName).
- **baseDir empty:** `LoadFromYAML(data, "")` with a `protocol_ref` → load error
  "requires loading from a project directory".
- **idempotent re-resolution:** after resolution, `ProtocolRef` is cleared; a
  second `resolveProtocolRefs` call is a no-op (no "mutually exclusive" false
  positive).
- **env overlay:** `LoadFromFile` with `CERBERUS_ENV` set, where the overlay
  introduces a `protocol_ref`, resolves it (and base refs stay resolved).
- **WS executor sees resolved protocol:** a resolved-from-file protocol with
  roles/auth drives `BuildWSProtocolIndex` the same as an inline one (one
  integration-shape test, mirroring `TestBuildWSProtocolIndex`).

## Relationship to M0 / M1 / M2 / M3

- **M1/M2** inline `protocol:` is the source of the file format; standalone
  files are that struct serialized, referenced by name.
- **This sub-project (M3-1)** is the artifact layer. M3-2 (Scout-generated WS
  cases) and M3-3 (`protocol infer`) will produce and consume these files; they
  remain deferred until dogfooding signals shape them (per the M3 proposal's
  trigger conditions).

## Open Questions

1. **File discovery beyond the conventional path.** Should `protocol_ref` ever
   accept a path (not just a name)? Deferred — the conventional
   `.cerberus/protocols/<name>.yaml` covers the proposal's intent; a path form
   can be added if a real need appears.
2. **Multiple services sharing one file.** Two services with the same
   `protocol_ref` both load the same file independently (no caching/dedup at
   load time). Cheap (small files); a cache can be added if profiling shows it
   matters.
3. **Overlay merge semantics for `Services`.** `mergo`'s slice merge determines
   how an env overlay's services combine with the base; this sub-project does
   not change that — it only ensures `protocol_ref` (from either source) is
   resolved after the merge.

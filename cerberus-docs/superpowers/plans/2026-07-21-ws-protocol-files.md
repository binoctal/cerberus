# WebSocket Realtime Engine (M3-1) — Standalone Protocol Files Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a service reference a standalone protocol description file (`.cerberus/protocols/<name>.yaml`) via `protocol_ref`, loaded and attached at config-load time, equivalent to an inline `protocol:` block.

**Architecture:** `Service` gains `ProtocolRef`. `LoadFromYAML` becomes dir-aware (`baseDir`); a new `resolveProtocolRefs` loads each referenced file into `svc.Protocol` before `Validate` (which is pure). Inline `protocol:` and `protocol_ref` are mutually exclusive. Resolution is idempotent (clears `ProtocolRef`) so the env-overlay re-validation path is safe (a defensive `resolveProtocolRefs` re-run is added to the env branch). Path traversal is rejected. No executor/validation-logic change — the resolved `Protocol` flows through existing `ValidateProtocol` and `BuildWSProtocolIndex`.

**Tech Stack:** Go 1.25 · stdlib (`os`, `path/filepath`, `strings`, `gopkg.in/yaml.v3`) · testify.

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, **pure Go (no CGo)**.
- **No new deps.** Uses only stdlib + already-vendored `gopkg.in/yaml.v3`.
- Commit author **`binoctal <binoctal@gmail.com>`**, **no** `Co-Authored-By`. Comments and commit messages in **English**.
- Documentation **only** in `cerberus-docs/`; **never** `docs/`.
- `make check` (fmt + lint + test `-race`) green. Tests in `internal/project/`, testify style, mirroring `internal/project/loader_test.go`.
- `LoadFromYAML` is in an internal package; its signature change ripples to exactly three callers (`loader.go` internal, `schema_test.go`, `smoke_test.go`) — each adds `, ""` when no `protocol_ref` is used.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-21-ws-protocol-files-design.md` (read for rationale).

---

## File Structure

- `internal/project/schema.go` — `Service.ProtocolRef` field (Task 1).
- `internal/project/loader.go` — `LoadFromYAML(data, baseDir)` signature; `resolveProtocolRefs`; `checkProtocolRefName`; env-overlay re-resolution (Task 1).
- `internal/project/loader_test.go` — protocol-file resolution + error tests (Task 1).
- `internal/project/schema_test.go`, `internal/smoke/smoke_test.go` — caller ripple `, ""` (Task 1).
- `cerberus-docs/configuration/project.md` — protocol / protocol_ref config reference (Task 2).
- `cerberus-docs/executors/websocket.md` — protocol_ref note in Protocol Declaration (Task 2).

---

## Task 1: protocol_ref schema + dir-aware loading + resolution + tests

**Files:**
- Modify: `internal/project/schema.go` (`Service`, ~line 17-27)
- Modify: `internal/project/loader.go` (`LoadFromYAML`, `LoadFromFile` incl. env branch, new helpers)
- Modify: `internal/project/schema_test.go:86`, `internal/smoke/smoke_test.go:116` (caller ripple)
- Test: `internal/project/loader_test.go` (append tests)

**Interfaces:**
- Produces: `LoadFromYAML(data []byte, baseDir string) (*Config, error)`; package-private `resolveProtocolRefs(cfg *Config, baseDir string) error`, `checkProtocolRefName(name string) error`; `Service.ProtocolRef string`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/project/loader_test.go`:

```go
func writeProtocolFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus", "protocols"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "protocols", name+".yaml"), []byte(content), 0644))
}

func TestLoadFromFile_ProtocolRefResolves(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: open-agents
`), 0644))
	writeProtocolFile(t, dir, "open-agents", "framing: json\ntype_path: data.event\n")

	cfg, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Protocol)
	assert.Equal(t, "json", cfg.Services[0].Protocol.Framing)
	assert.Equal(t, "data.event", cfg.Services[0].Protocol.TypePath)
	// ProtocolRef cleared after resolution (idempotent re-resolution).
	assert.Equal(t, "", cfg.Services[0].ProtocolRef)
}

func TestLoadFromFile_ProtocolInlineUnchanged(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol: { framing: text, type_path: type }
`), 0644))
	cfg, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Protocol)
	assert.Equal(t, "text", cfg.Services[0].Protocol.Framing)
	assert.Equal(t, "", cfg.Services[0].ProtocolRef)
}

func TestLoadFromFile_ProtocolRefMutuallyExclusive(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol: { framing: json }
    protocol_ref: x
`), 0644))
	_, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestLoadFromFile_ProtocolRefMissingFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: ghost
`), 0644))
	_, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestLoadFromFile_ProtocolRefUnparseable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: bad
`), 0644))
	// Invalid YAML: a mapping value nested under a scalar.
	writeProtocolFile(t, dir, "bad", "framing: json: oops\n")
	_, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestLoadFromFile_ProtocolRefPathTraversal(t *testing.T) {
	for _, ref := range []string{"../../etc/passwd", "a/b", ".."} {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: "`+ref+`"
`), 0644))
		_, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
		require.Error(t, err, "ref %q should be rejected", ref)
		assert.Contains(t, err.Error(), "protocol_ref")
	}
}

func TestLoadFromYAML_BaseDirEmptyProtocolRef(t *testing.T) {
	_, err := LoadFromYAML([]byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: x
`), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project directory")
}

func TestResolveProtocolRefs_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeProtocolFile(t, dir, "p", "framing: json\n")
	cfg := &Config{Services: []Service{{Name: "rt", URL: "http://x", ProtocolRef: "p"}}}
	require.NoError(t, resolveProtocolRefs(cfg, dir))
	require.NotNil(t, cfg.Services[0].Protocol)
	assert.Equal(t, "", cfg.Services[0].ProtocolRef)
	// Re-running must be a no-op (no false "mutually exclusive" error).
	require.NoError(t, resolveProtocolRefs(cfg, dir))
}

// TestLoadFromFile_ProtocolRefSurvivesEnvOverlay is a regression guard: a base
// protocol_ref (resolved by the initial LoadFromYAML) stays resolved through the
// env-overlay merge + re-validation. It also exercises the env branch's
// defensive resolveProtocolRefs re-run (idempotent for already-resolved refs).
func TestLoadFromFile_ProtocolRefSurvivesEnvOverlay(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: p
settings:
  confidence_threshold: 0.8
`), 0644))
	writeProtocolFile(t, dir, "p", "framing: json\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.staging.yaml"), []byte(`
settings:
  confidence_threshold: 0.95
`), 0644))
	t.Setenv("CERBERUS_ENV", "staging")

	cfg, err := LoadFromFile(filepath.Join(dir, "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Protocol)
	assert.Equal(t, "json", cfg.Services[0].Protocol.Framing)
	assert.InDelta(t, 0.95, cfg.Settings.ConfidenceThreshold, 0.01)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -run 'TestLoadFromFile_ProtocolRef|TestLoadFromYAML_BaseDirEmptyProtocolRef|TestResolveProtocolRefs_Idempotent' -v ./internal/project/`
Expected: FAIL — `Service.ProtocolRef` and `resolveProtocolRefs` do not exist (compile error), and `LoadFromYAML` still takes one argument. (The env-overlay test is a regression guard and may pass once the core resolves; that is fine — its job is to lock the behavior in.)

- [ ] **Step 3: Write minimal implementation**

In `internal/project/schema.go`, add the field to `Service` (after the `Protocol` field):

```go
	// Protocol optionally declares this service's WebSocket protocol facts.
	// When nil, the WS executor falls back to M0 behavior.
	Protocol *Protocol `yaml:"protocol,omitempty"`
	// ProtocolRef optionally names a standalone protocol description file
	// (.cerberus/protocols/<name>.yaml) loaded as this service's Protocol.
	// Mutually exclusive with Protocol (inline). Empty means use Protocol (or none).
	ProtocolRef string `yaml:"protocol_ref,omitempty"`
```

In `internal/project/loader.go`, change `LoadFromYAML` to take `baseDir` and call `resolveProtocolRefs` before `Validate`:

```go
func LoadFromYAML(data []byte, baseDir string) (*Config, error) {
	interpolated := envVarRE.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1])
		if val := os.Getenv(varName); val != "" {
			return []byte(val)
		}
		return match
	})

	var cfg Config
	if err := yaml.Unmarshal(interpolated, &cfg); err != nil {
		return nil, fmt.Errorf("parse project config: %w", err)
	}

	applyDefaults(&cfg)
	if err := resolveProtocolRefs(&cfg, baseDir); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
```

In `LoadFromFile`, pass the directory to `LoadFromYAML`:

```go
	cfg, err := LoadFromYAML(data, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
```

And in the env-overlay branch, after `applyDefaults(cfg)` and before `cfg.Validate()`, add a defensive re-resolution (idempotent for already-resolved base refs; resolves any overlay-introduced ref):

```go
			if err := mergo.Merge(cfg, overlay, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("merge env overlay: %w", err)
			}
			// Re-apply defaults in case overlay zeroed fields that had defaults.
			applyDefaults(cfg)
			// Re-resolve protocol_ref in case the overlay introduced one; base
			// refs were already resolved (and cleared) by LoadFromYAML, so this
			// is a no-op for them.
			if err := resolveProtocolRefs(cfg, filepath.Dir(path)); err != nil {
				return nil, err
			}
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
```

Add the resolution helpers (after `LoadFromFile`, before `applyDefaults`):

```go
// resolveProtocolRefs loads each service's referenced protocol description
// file (.cerberus/protocols/<name>.yaml under baseDir) into svc.Protocol. It is
// called after applyDefaults and before Validate. Inline protocol and
// protocol_ref are mutually exclusive. baseDir == "" means files cannot be
// resolved (a protocol_ref then errors). On success the ref is cleared so the
// function is idempotent (the env-overlay re-validation path re-runs it).
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
		svc.ProtocolRef = ""
	}
	return nil
}

// checkProtocolRefName rejects a protocol_ref that could escape the protocols
// directory (path traversal). The ref must be a plain name.
func checkProtocolRefName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("must be a plain name (no path separators or parent traversal)")
	}
	return nil
}
```

Add `"strings"` to loader.go's imports (it is not currently imported there). The existing imports (`fmt`, `os`, `path/filepath`, `regexp`, `dario.cat/mergo`, `gopkg.in/yaml.v3`) remain.

Update the two external callers:
- `internal/project/schema_test.go:86`: `LoadFromYAML([]byte(input))` → `LoadFromYAML([]byte(input), "")`
- `internal/smoke/smoke_test.go:116`: `project.LoadFromYAML([]byte(yaml))` → `project.LoadFromYAML([]byte(yaml), "")`

- [ ] **Step 4: Run tests to verify they pass (incl. regression)**

Run: `go test ./internal/project/ -v -run 'TestLoadFromFile|TestLoadFromYAML|TestResolveProtocolRefs|TestValidate'`
Expected: PASS — new protocol-file tests pass; existing loader/schema/validate tests pass (the `, ""` ripple is a no-op for configs without protocol_ref).

Then the full project package + smoke:
Run: `go test ./internal/project/ ./internal/smoke/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/project/schema.go internal/project/loader.go internal/project/loader_test.go internal/project/schema_test.go internal/smoke/smoke_test.go
git commit -m "feat(project): load protocol declarations from standalone files via protocol_ref"
```

---

## Task 2: Document protocol_ref (config reference + executor doc)

**Files:**
- Modify: `cerberus-docs/configuration/project.md` (add protocol / protocol_ref config reference)
- Modify: `cerberus-docs/executors/websocket.md` (Protocol Declaration: protocol_ref note)

**Interfaces:** none (docs).

- [ ] **Step 1: Add the config reference to project.md**

In `cerberus-docs/configuration/project.md`, add a section describing how a service declares its WS protocol — inline (`protocol:`) or by file reference (`protocol_ref:`). (The doc currently has no protocol section.) Add:

```markdown
### WebSocket protocol

A service may declare its WebSocket protocol facts so the executor injects auth,
matches by the declared routing field, and frames messages deterministically
(see [WebSocket executor](../executors/websocket.md)). Declare it **inline**:

```yaml
services:
  - name: rt
    url: http://localhost:8787
    protocol:
      framing: json
      type_path: type
      auth: { strategy: query, param: token, credential_ref: web-actor }
```

…or **by reference** to a standalone, shareable file under
`.cerberus/protocols/<name>.yaml`:

```yaml
services:
  - name: rt
    url: http://localhost:8787
    protocol_ref: open-agents
```

```yaml
# .cerberus/protocols/open-agents.yaml — version-controlled, shareable across projects
framing: json
type_path: type
auth:
  strategy: query
  param: token
  credential_ref: web-actor
roles:
  web: { credential_ref: web-actor, params: { type: web } }
```

`protocol` and `protocol_ref` are mutually exclusive. The reference is a plain
name (no path separators or `..`); the file is loaded at config-load time. Full
field semantics (framing, type_path, auth, roles, handshake, assertions) are in
the [WebSocket executor doc](../executors/websocket.md).
```

- [ ] **Step 2: Add the protocol_ref note to websocket.md**

In `cerberus-docs/executors/websocket.md`, in the `## Protocol Declaration` section (e.g. at its start, or after the `### Executor-authoritative auth` subsection), add:

```markdown
### Inline or referenced

The protocol declaration may be written **inline** on the service (`protocol:`),
or **referenced** by name (`protocol_ref: <name>`) from a standalone file
`.cerberus/protocols/<name>.yaml` loaded at config time. The two are mutually
exclusive. A referenced file is a YAML serialization of the same `Protocol`
fields documented here (framing, type_path, auth, roles) and behaves identically
once loaded. See the [project configuration reference](../configuration/project.md).
```

- [ ] **Step 3: Verify build + lint + test green + audit**

Run: `make check`
Expected: exit 0 (docs-only change).

Run: `grep -rn "protocol_ref" cerberus-docs/ internal/head/agent/prompts.go`
Expected: matches only in the new project.md / websocket.md docs (the feature is config-load-time, not LLM-facing, so the steer prompt need not mention it).

- [ ] **Step 4: Commit**

```bash
git add cerberus-docs/configuration/project.md cerberus-docs/executors/websocket.md
git commit -m "docs(ws): document standalone protocol files (protocol_ref)"
```

---

## Final Verification

- [ ] `make check` green across the whole branch.
- [ ] `go build ./...` clean (the `LoadFromYAML` signature change is internal; all callers updated — loader.go, schema_test.go, smoke_test.go).
- [ ] `-race` clean (no new concurrency; pure file IO).

## Self-Review Notes

- **Spec coverage:** D1 (new `protocol_ref` field) → Task 1 ✓; D2 (conventional `.cerberus/protocols/<name>.yaml`, version-controlled) → Task 1 + Task 2 ✓; D3 (dir-aware `LoadFromYAML`, `resolveProtocolRefs` before Validate, idempotent clear, path-traversal guard) → Task 1 ✓; D4 (env overlay re-resolution, idempotent) → Task 1 (folded into the env branch) ✓; D5 (no executor/validation-logic change) → Task 1 touches only schema+loader ✓.
- **Type consistency:** `Service.ProtocolRef string`; `resolveProtocolRefs(cfg *Config, baseDir string) error`; `LoadFromYAML(data []byte, baseDir string)` — names match across the task.
- **Caller ripple:** exactly `loader.go` (internal), `schema_test.go`, `smoke_test.go` — all updated in Task 1.
- **Coverage gap (known):** the "env overlay INTRODUCES a new protocol_ref" path is not tested deterministically because it depends on mergo's slice-merge semantics (uncertain); correctness is guaranteed by the defensive `resolveProtocolRefs` re-run in the env branch + the idempotency test (TestResolveProtocolRefs_Idempotent). The env-overlay test present (TestLoadFromFile_ProtocolRefSurvivesEnvOverlay) is a regression guard for the base case.
- **No placeholders:** every code step shows complete code; every test step shows complete test bodies.

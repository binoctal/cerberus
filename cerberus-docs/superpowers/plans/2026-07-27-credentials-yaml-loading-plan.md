# credentials.yaml Loading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `.cerberus/credentials.yaml` actually load — its Token/Email/Password merge into actors so static-token WS actors (e.g. `web`/`demo_token`) reach the executor with a token.

**Architecture:** One new load step (`mergeCredentials`) inside `LoadFromFile`, layered after the env-overlay block. Precedence: env (`CERBERUS_ACTOR_*`) > `credentials.yaml` > `project.yaml` inline, applied per-field. `ResolveCredentials` gains a `_TOKEN` env branch; its signature and all three call sites are untouched.

**Tech Stack:** Go 1.25, `gopkg.in/yaml.v3`, `github.com/stretchr/testify` (assert/require), `github.com/spf13/cobra`.

**Spec:** `cerberus-docs/superpowers/specs/2026-07-27-credentials-yaml-loading-design.md`

## Global Constraints

- Go 1.25, module `github.com/binoctal/cerberus`, pure-Go (no CGo; SQLite via `modernc.org/sqlite`).
- Commit author: `binoctal <binoctal@gmail.com>`; **NO** `Co-Authored-By` / `Co-authored-By`.
- Code comments and commit messages in English.
- `make check` (fmt + lint + test -race) must EXIT 0.
- All docs in `cerberus-docs/`; never create anything in `docs/`.
- TDD discipline per task: write failing test → run it red → implement minimal code → run it green → commit.
- No new dependencies; loader.go already imports `fmt`, `os`, `path/filepath`, `gopkg.in/yaml.v3`.

## File Structure

- `internal/project/loader.go` — `LoadFromFile` gains a `mergeCredentials(cfg, configDir)` call; new unexported types `credentialsFile` / `credentialSecret` + the `mergeCredentials` function live here (same file as the sibling load-step `resolveProtocolRefs`).
- `internal/project/loader_test.go` — six new tests + one `mustActor` test helper.
- `internal/project/credentials.go` — `ResolveCredentials` gains a `_TOKEN` env branch (signature unchanged).
- `internal/project/credentials_test.go` — one new `_TOKEN` env test.
- `cmd/cerberus/init.go` — `credentials.yaml` template advertises the `token:` field.
- `cerberus-docs/configuration/credentials.md` — document `_TOKEN` env and the `token:` field.

---

## Task 1: LoadFromFile merges credentials.yaml (the core fix)

This is the bug fix: the static-token actor's `Token` now reaches `cfg.Actors[].Credentials.Token`, which the existing `BuildWSProtocolIndex` static fallback (`ws_protocol.go:142-145`) feeds into `ActorTokens`.

**Files:**
- Modify: `internal/project/loader.go` (add types + `mergeCredentials` after `resolveProtocolRefs`; call it inside `LoadFromFile` before `return cfg`).
- Modify: `internal/project/loader_test.go` (add `mustActor` helper + six tests).

**Interfaces:**
- Consumes: `Config.Actors []Actor` and `Actor.Credentials CredentialRef` (defined in `schema.go:33,41`); `configDir` already computed in `LoadFromFile` at `loader.go:51`.
- Produces: populated `Actor.Credentials.{Email,Password,Token}` after `LoadFromFile` returns. Downstream consumers (`ResolveCredentials`, `BuildWSProtocolIndex`) are unchanged and already read these fields.

- [ ] **Step 1: Add the `mustActor` test helper to `loader_test.go`**

Append near the bottom of `internal/project/loader_test.go`:

```go
// mustActor returns the actor named name, failing the test if it is absent.
// Shared by the credentials.yaml tests below.
func mustActor(t *testing.T, cfg *Config, name string) Actor {
	t.Helper()
	for _, a := range cfg.Actors {
		if a.Name == name {
			return a
		}
	}
	t.Fatalf("actor %q not found in config", name)
	return Actor{}
}
```

- [ ] **Step 2: Write the first failing test — merges all three fields**

Append to `internal/project/loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_MergesAll(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
project:
  name: relay
actors:
  - name: web
  - name: bridge
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials.yaml"), []byte(`
actors:
  web:
    email: web@x.dev
    password: webpw
    token: demo_token
  bridge:
    email: bridge@x.dev
    password: bridgepw
    token: deviceToken
`), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	web := mustActor(t, cfg, "web")
	assert.Equal(t, "web@x.dev", web.Credentials.Email)
	assert.Equal(t, "webpw", web.Credentials.Password)
	assert.Equal(t, "demo_token", web.Credentials.Token)

	bridge := mustActor(t, cfg, "bridge")
	assert.Equal(t, "deviceToken", bridge.Credentials.Token)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_MergesAll -v`
Expected: FAIL — `web.Credentials.Token` is `""`, not `"demo_token"` (credentials.yaml is not loaded yet).

- [ ] **Step 4: Add the types and `mergeCredentials` to `loader.go`**

Insert these unexported definitions after the `resolveProtocolRefs` function (before `applyDefaults`, around `loader.go:132`):

```go
// credentialsFile is the on-disk shape of .cerberus/credentials.yaml: a map
// keyed by actor name. It is intentionally distinct from project.yaml's actor
// list form.
type credentialsFile struct {
	Actors map[string]credentialSecret `yaml:"actors"`
}

// credentialSecret mirrors the subset of CredentialRef that credentials.yaml
// may set: Email, Password, and the static WS Token.
type credentialSecret struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
}

// mergeCredentials loads .cerberus/credentials.yaml from configDir (alongside
// the project config) and, for every actor present in both cfg and the file,
// overrides the actor's Email/Password/Token with the non-empty values from
// the file (layered override; env still wins via ResolveCredentials). A missing
// file is not an error — it is optional and gitignored. A present but malformed
// file is an error (fail loud), mirroring the env-overlay malformed handling.
func mergeCredentials(cfg *Config, configDir string) error {
	credPath := filepath.Join(configDir, "credentials.yaml")
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // optional file
		}
		return fmt.Errorf("read credentials file: %w", err)
	}
	var cf credentialsFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return fmt.Errorf("parse credentials file %s: %w", credPath, err)
	}
	for i := range cfg.Actors {
		sec, ok := cf.Actors[cfg.Actors[i].Name]
		if !ok {
			continue
		}
		if sec.Email != "" {
			cfg.Actors[i].Credentials.Email = sec.Email
		}
		if sec.Password != "" {
			cfg.Actors[i].Credentials.Password = sec.Password
		}
		if sec.Token != "" {
			cfg.Actors[i].Credentials.Token = sec.Token
		}
	}
	return nil
}
```

- [ ] **Step 5: Call `mergeCredentials` from `LoadFromFile`**

In `LoadFromFile`, replace the final `return cfg, nil` (the one after the env-overlay `if` block, around `loader.go:95`) with:

```go
	if err := mergeCredentials(cfg, configDir); err != nil {
		return nil, err
	}

	return cfg, nil
```

`configDir` is already in scope (`loader.go:51`).

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_MergesAll -v`
Expected: PASS.

- [ ] **Step 7: Write the layered-override test**

Append to `loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_OverridesInline(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
actors:
  - name: web
    credentials:
      email: inline@x.dev
      password: inlinepw
      token: inline_tok
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials.yaml"), []byte(`
actors:
  web:
    email: file@x.dev
    password: filepw
    token: file_tok
`), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	a := mustActor(t, cfg, "web")
	assert.Equal(t, "file@x.dev", a.Credentials.Email)
	assert.Equal(t, "filepw", a.Credentials.Password)
	assert.Equal(t, "file_tok", a.Credentials.Token)
}
```

- [ ] **Step 8: Run it — expect PASS immediately (no new code needed)**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_OverridesInline -v`
Expected: PASS (confirms the per-field override implemented in Step 4).

- [ ] **Step 9: Write the per-field-override test**

Append to `loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_PerFieldOverride(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
actors:
  - name: web
    credentials:
      email: inline@x.dev
      password: inlinepw
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials.yaml"), []byte(`
actors:
  web:
    token: demo_token
`), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	a := mustActor(t, cfg, "web")
	assert.Equal(t, "inline@x.dev", a.Credentials.Email, "inline email preserved")
	assert.Equal(t, "inlinepw", a.Credentials.Password, "inline password preserved")
	assert.Equal(t, "demo_token", a.Credentials.Token, "token from file")
}
```

- [ ] **Step 10: Run it — expect PASS**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_PerFieldOverride -v`
Expected: PASS.

- [ ] **Step 11: Write the missing-file test**

Append to `loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_Missing_NoError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
actors:
  - name: web
    credentials:
      token: inline_tok
`), 0644))
	// No credentials.yaml present.

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	a := mustActor(t, cfg, "web")
	assert.Equal(t, "inline_tok", a.Credentials.Token)
}
```

- [ ] **Step 12: Run it — expect PASS**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_Missing_NoError -v`
Expected: PASS.

- [ ] **Step 13: Write the malformed-file test**

Append to `loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_Malformed_Error(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
actors:
  - name: web
`), 0644))
	// Unclosed flow sequence → definite YAML parse error.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials.yaml"), []byte("actors: [unterminated"), 0644))

	_, err := LoadFromFile(cfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials")
}
```

- [ ] **Step 14: Run it — expect PASS**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_Malformed_Error -v`
Expected: PASS.

- [ ] **Step 15: Write the `.cerberus/`-location test (path-resolution guard)**

Append to `loader_test.go`:

```go
func TestLoadFromFile_CredentialsYAML_AtCerberusLocation(t *testing.T) {
	dir := t.TempDir()
	cerbDir := filepath.Join(dir, ".cerberus")
	require.NoError(t, os.MkdirAll(cerbDir, 0755))
	cfgPath := filepath.Join(cerbDir, "project.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`
actors:
  - name: web
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(cerbDir, "credentials.yaml"), []byte(`
actors:
  web:
    token: demo_token
`), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	a := mustActor(t, cfg, "web")
	assert.Equal(t, "demo_token", a.Credentials.Token)
}
```

- [ ] **Step 16: Run it — expect PASS**

Run: `go test ./internal/project/ -run TestLoadFromFile_CredentialsYAML_AtCerberusLocation -v`
Expected: PASS.

- [ ] **Step 17: Run the whole project package + fmt + lint**

Run: `make fmt && go test ./internal/project/ -race -v`
Expected: all green, including pre-existing tests (`TestLoadFromFile_NoEnv`, `TestLoadFromFile_EnvOverlay*`, etc.).

- [ ] **Step 18: Commit**

```bash
git add internal/project/loader.go internal/project/loader_test.go
git commit -m "fix(project): load .cerberus/credentials.yaml and merge actor Token/Email/Password"
```

---

## Task 2: ResolveCredentials adds `_TOKEN` env

Adds the env override for Token (env is the highest-precedence tier). Independent of Task 1's file load; signature unchanged so no call-site changes.

**Files:**
- Modify: `internal/project/credentials.go:9-24` (add a `_TOKEN` branch).
- Modify: `internal/project/credentials_test.go` (add one test).

**Interfaces:**
- Consumes: `Actor.Credentials.Token` (may already be populated by Task 1's file merge).
- Produces: `ResolveCredentials` still returns `*Config`; no caller changes.

- [ ] **Step 1: Write the failing test**

Append to `internal/project/credentials_test.go`:

```go
func TestResolveCredentials_TokenEnv(t *testing.T) {
	t.Setenv("CERBERUS_ACTOR_WEB_TOKEN", "env_tok")

	cfg := &Config{
		Actors: []Actor{
			{Name: "web", Credentials: CredentialRef{Token: "file_tok"}},
		},
	}
	resolved := ResolveCredentials(cfg)
	assert.Equal(t, "env_tok", resolved.Actors[0].Credentials.Token)
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/project/ -run TestResolveCredentials_TokenEnv -v`
Expected: FAIL — `Token` stays `"file_tok"` (env not read for Token yet).

- [ ] **Step 3: Add the `_TOKEN` branch to `ResolveCredentials`**

In `internal/project/credentials.go`, add this block inside the `for` loop, right after the `_PASSWORD` block:

```go
		if token := os.Getenv(envPrefix + "_TOKEN"); token != "" {
			actor.Credentials.Token = token
		}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/project/ -run TestResolveCredentials_TokenEnv -v`
Expected: PASS.

- [ ] **Step 5: Confirm existing credentials tests still green**

Run: `go test ./internal/project/ -run "TestResolveActorCredentials|TestEnvOverridesCredentialFile|TestCredentialFileFallback|TestCredentialRef_TokenRoundTrip" -v`
Expected: all PASS (env > inline semantics preserved).

- [ ] **Step 6: Commit**

```bash
git add internal/project/credentials.go internal/project/credentials_test.go
git commit -m "feat(project): support CERBERUS_ACTOR_<NAME>_TOKEN env override"
```

---

## Task 3: Advertise `token:` in `cerberus init` + document it

The mechanism now works end-to-end; make it discoverable so users know `token:` exists in `credentials.yaml` and `_TOKEN` exists as env. No unit test — verified by a `cerberus init` smoke check.

**Files:**
- Modify: `cmd/cerberus/init.go:61-67` (template).
- Modify: `cerberus-docs/configuration/credentials.md`.

**Interfaces:** None (template string + prose docs).

- [ ] **Step 1: Add the `# token:` hint to the init template**

In `cmd/cerberus/init.go`, replace the `credYAML` literal (the block starting `# Credentials — DO NOT commit this file`) with:

```go
			credYAML := `# Credentials — DO NOT commit this file
# Add to .gitignore
actors:
  admin:
    email: admin@example.com
    password: changeme
    # token: <static WS token — for actors with no auth flow (API key / dev backdoor)>
`
```

- [ ] **Step 2: Update `credentials.md` — env section + token field**

In `cerberus-docs/configuration/credentials.md`:

Replace the env-variables example block (the `export CERBERUS_ACTOR_ADMIN_EMAIL` / `_PASSWORD` lines) with one that also shows `_TOKEN`:

```bash
export CERBERUS_ACTOR_ADMIN_EMAIL="admin@example.com"
export CERBERUS_ACTOR_ADMIN_PASSWORD="secret"
export CERBERUS_ACTOR_ADMIN_TOKEN="api-key-or-dev-token"
```

And in the Credentials File example, add a `token:` line:

```yaml
actors:
  admin:
    email: admin@example.com
    password: changeme
    token: api-key-or-dev-token
```

Leave the Priority Order section as-is (env > credentials.yaml > project config) — this fix makes it true.

- [ ] **Step 3: Smoke-check `cerberus init`**

Run (build from the repo, then run the binary in a temp dir so `init` writes there):

```bash
cd /home/mason/Documents/code_projects/private/cerberus && go build -o /tmp/cerberus-init-smoke ./cmd/cerberus
tmp=$(mktemp -d) && (cd "$tmp" && /tmp/cerberus-init-smoke init) && grep -q "# token:" "$tmp/.cerberus/credentials.yaml" && echo OK
rm -rf "$tmp" /tmp/cerberus-init-smoke
```

Expected: prints `OK` (the generated template contains the token hint). Also confirm `project.yaml` and `.gitignore` are still written (the command prints `✓ Created ...` lines).

- [ ] **Step 4: fmt + build**

Run: `make fmt && make build`
Expected: build succeeds, no fmt diff.

- [ ] **Step 5: Commit**

```bash
git add cmd/cerberus/init.go cerberus-docs/configuration/credentials.md
git commit -m "docs(project): advertise token field in credentials.yaml template and docs"
```

---

## Task 4: Verification gate

Prove the whole change holds together: the unit suite is clean, and the original regression (device:online relay) passes live.

**Files:** None (verification only).

- [ ] **Step 1: Full repo check (hard gate)**

Run: `make check`
Expected: EXIT 0 (fmt + lint + `go test -race ./...`). If lint or any test fails, fix before proceeding — do not claim done.

- [ ] **Step 2: Confirm the regression-chain unit coverage**

Run: `go test ./internal/project/ ./internal/head/agent/ -run "CredentialsYAML|ResolveCredentials_TokenEnv|BuildWSProtocolIndex_StaticToken" -v`
Expected: all PASS. Together these prove: file Token → `cfg.Actors[].Credentials.Token` (Task 1), env Token override (Task 2), and Token → `ActorTokens` (`TestBuildWSProtocolIndex_StaticToken`).

- [ ] **Step 3: Live dogfood rerun (the actual regression)**

Follow `cerberus-docs/technical/dogfood/2026-07-24-ws-relay-live-execution-dogfood.md`:

1. Start the open-agents dev server on `:8989` (from `../open-agents/apps/api`: `fnm use 22 && npm run dev`).
2. Provision via `POST /api/dev/setup` with an `Origin` header.
3. Write `.cerberus/project.yaml` (relay config) and `.cerberus/credentials.yaml` with `web: {token: demo_token}` and `bridge: {token: <deviceToken>}`.
4. `make build`.
5. `cerberus run --goal "device:online peer-join relay"`.

Expected: the `ws-realtime-relay-web-signal-device-online` verdict is **PASS** — `web` receives the relayed `device:online` event because its `demo_token` now reaches `tokenFor`.

- [ ] **Step 4: Record outcome**

If PASS: note it in the session summary and update cccmemory `credentials-yaml-not-loaded-bug` confidence to `verified`. No commit needed (verification only). If FAIL: do not claim success — capture the failure trace and re-enter systematic-debugging.

---

## Self-Review (run before handing off)

- **Spec coverage:** Task 1 = LoadFromFile merge (spec §"LoadFromFile change", §"credentials.yaml schema"). Task 2 = `_TOKEN` env (§"ResolveCredentials change"). Task 3 = init template + docs (§"`cerberus init` template", Files→`credentials.md`). Task 4 = Verification (spec §Verification). What stays unchanged (§"What stays unchanged") needs no task. All sections covered.
- **Placeholders:** none — every code step shows the exact code; tests are complete.
- **Type consistency:** `credentialsFile` / `credentialSecret` defined in Task 1 Step 4 and used there only; `mustActor` defined Task 1 Step 1 and reused across Task 1 tests with the same signature; `ResolveCredentials` signature unchanged across tasks; merge field names (`Email`/`Password`/`Token`) match `CredentialRef` (`schema.go:41`).
- **Ordering:** Task 1 before Task 2 (so the precedence chain is coherent, though Task 2 does not technically depend on Task 1); Task 3 last (documents final behavior); Task 4 after all code lands.

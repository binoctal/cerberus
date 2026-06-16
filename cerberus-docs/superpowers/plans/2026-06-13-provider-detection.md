# LLM Provider Detection by CLI Identity — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Determine the LLM provider from which host CLI launched cerberus (Claude Code today) instead of guessing from the model name.

**Architecture:** New `internal/detect` package exposes a `Detector` registry; `ClaudeCodeDetector` recognizes the `CLAUDECODE` env var Claude Code injects into every subprocess. `config.Load()` runs detection once and resolves `LLMProvider` as explicit-env > detected-CLI > model-name. The base-URL and API-key resolvers become prefix-aware (read the detected CLI's credential env prefix first) while keeping model-name inference as a graceful fallback, so behavior for unknown CLIs is byte-for-byte unchanged and no test becomes environment-fragile. `internal/llm` already trusts a non-empty `Provider` — no code change there, only a regression check.

**Tech Stack:** Go 1.25, module `github.com/binoctal/cerberus`, `testing` + `github.com/stretchr/testify`.

**Spec:** `docs/superpowers/specs/2026-06-13-provider-detection-design.md`

---

## Design refinement (read before implementing)

The spec §2 said resolvers become prefix-parameterized. Implementation choice: **prefix-first with model-name fallback**, not prefix-replaces-model. Rationale discovered while planning:

- A known CLI's credential prefix is consulted first (Claude Code → `ANTHROPIC_*`).
- Model-name inference (`gpt`→`OPENAI`, `gemini`→`GEMINI`, else `ANTHROPIC`) is retained as a fallback when the prefix yields nothing.
- This is a behavioral **superset** of today: the normal case (Claude Code + glm-5.1) is identical, AND existing model-fallback tests keep passing even when run under Claude Code (where `CLAUDECODE` is set). A pure "prefix replaces model" design would break `TestAPIKeyAutoDetect` under Claude Code because detection would force the `ANTHROPIC` key for a `gpt` model.

`session/lifecycle.go:110` (`SetupHeadDrivers`, per-head clients) is intentionally left unchanged: it relies on `detectProvider(model)`, which is correct for every Anthropic-compatible model (claude, glm). Threading `Provider` through the session constructor is deferred until a per-head non-Anthropic model actually exists (YAGNI).

## File Structure

- **Create** `internal/detect/detect.go` — `CLI`, `Profile`, `Detector` interface, `ClaudeCodeDetector`, registry `Detect()`.
- **Create** `internal/detect/detect_test.go` — detector hit/miss + registry tests.
- **Modify** `internal/config/config.go` — `Config.CLIProfile` field; `Load()` detects and resolves provider priority; `resolveBaseURL`/`resolveAPIKey` take a `detect.Profile`; new `providerKey` helper. `resolveModel` unchanged.
- **Modify** `internal/config/config_test.go` — add provider-priority tests.
- **Modify** `internal/config/settings_test.go` — update direct resolver call signatures; add prefix-path tests; add `detect` import.
- **No change** `internal/llm/client.go` — already trusts non-empty `Provider`. Existing `TestNewClientWithConfig_ProviderOverride` covers it.

## Prerequisite

- [x] **Step 0: Start from a clean working tree**

The working tree currently holds uncommitted v0.7.x work (deep-binding feature + 3 bug fixes + tests). Commit or stash it first so each task below commits cleanly.

```bash
git status --short          # review the uncommitted changes
# Commit or stash them per your preference (author: binoctal, no Co-Authored-By).
git status --short          # confirm clean before starting Task 1
```

---

### Task 1: `internal/detect` package

**Files:**
- Create: `internal/detect/detect.go`
- Create: `internal/detect/detect_test.go`

- [x] **Step 1: Write the failing tests**

Create `internal/detect/detect_test.go`:

```go
package detect

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClaudeCodeDetector_HitsWhenEnvSet(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.True(t, ok)
	assert.Equal(t, CLIClaudeCode, p.CLI)
	assert.Equal(t, "anthropic", p.Provider)
	assert.Equal(t, "ANTHROPIC", p.EnvPrefix)
	assert.Equal(t, ".claude/settings.json", p.SettingsFile)
}

func TestClaudeCodeDetector_MissesWhenEnvUnset(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p, ok := ClaudeCodeDetector{}.Detect()
	assert.False(t, ok)
	assert.Equal(t, CLI(""), p.CLI, "miss returns a zero-value Profile")
}

func TestDetect_ReturnsClaudeCodeWhenSet(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	p := Detect()
	assert.Equal(t, CLIClaudeCode, p.CLI)
	assert.Equal(t, "anthropic", p.Provider)
}

func TestDetect_ReturnsUnknownWhenNothingSet(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	p := Detect()
	assert.Equal(t, CLIUnknown, p.CLI)
	assert.Equal(t, "", p.Provider, "unknown profile has empty provider so callers fall back")
	assert.Equal(t, "", p.EnvPrefix)
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/detect/ -run 'TestDetect|TestClaudeCodeDetector' -v`
Expected: FAIL / build error — `undefined: CLI`, `ClaudeCodeDetector`, `Detect`, etc.

- [x] **Step 3: Write the implementation**

Create `internal/detect/detect.go`:

```go
// Package detect identifies the host CLI that launched cerberus (Claude Code,
// Codex, Gemini CLI) from the process environment. Detection answers "which
// CLI am I under", which deterministically implies the LLM provider protocol,
// the credential env prefix, and the config file — no model-name guessing.
package detect

import "os"

// CLI identifies the host CLI that launched cerberus.
type CLI string

const (
	CLIClaudeCode CLI = "claude-code"
	CLIUnknown    CLI = "unknown"
)

// Profile bundles everything a detected host CLI implies.
type Profile struct {
	CLI          CLI
	Provider     string // "anthropic" | "openai" | "gemini"
	EnvPrefix    string // "ANTHROPIC" | "OPENAI" | "GEMINI"
	SettingsFile string // config file the CLI owns, e.g. ".claude/settings.json"
}

// Detector recognizes a single host CLI. Detect returns the Profile and true
// when this CLI is the active host.
type Detector interface {
	Detect() (Profile, bool)
}

// ClaudeCodeDetector recognizes Claude Code by the CLAUDECODE env var it
// injects into every subprocess (Bash tool, "!" command, "cerberus mcp",
// subagents). Claude Code always speaks the Anthropic protocol regardless of
// the underlying model, so this is more reliable than inferring the provider
// from the model name.
type ClaudeCodeDetector struct{}

func (ClaudeCodeDetector) Detect() (Profile, bool) {
	if os.Getenv("CLAUDECODE") == "" {
		return Profile{}, false
	}
	return Profile{
		CLI:          CLIClaudeCode,
		Provider:     "anthropic",
		EnvPrefix:    "ANTHROPIC",
		SettingsFile: ".claude/settings.json",
	}, true
}

// detectors is the ordered registry consulted by Detect. Adding a CLI later
// (e.g. Codex) means appending one detector here — no other change required.
var detectors = []Detector{
	ClaudeCodeDetector{},
}

// Detect runs the detector registry and returns the first hit. When no
// detector recognizes the host, it returns an Unknown profile with empty
// fields so callers fall back to their existing model-name behavior unchanged.
func Detect() Profile {
	for _, d := range detectors {
		if p, ok := d.Detect(); ok {
			return p
		}
	}
	return Profile{CLI: CLIUnknown}
}
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/detect/ -v`
Expected: PASS — 4 tests.

- [x] **Step 5: Commit**

```bash
git add internal/detect/detect.go internal/detect/detect_test.go
git commit -m "feat(detect): add CLI identity detection package"
```

---

### Task 2: Parameterize `resolveBaseURL` by `Profile`

**Files:**
- Modify: `internal/config/config.go` (import detect; `resolveBaseURL` signature; `Load` call site)
- Modify: `internal/config/settings_test.go` (update calls; add prefix test; add import)

- [x] **Step 1: Update the resolver tests (signature change makes them fail to compile)**

In `internal/config/settings_test.go`, add the import and update the two `resolveBaseURL` calls, then add a prefix-path test.

Add to the import block:
```go
import (
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/detect"
)
```

Change (in `TestResolveBaseURL_Priority`):
```go
if got := resolveBaseURL(settings); got != "env-url" {
```
to:
```go
if got := resolveBaseURL(settings, detect.Profile{}); got != "env-url" {
```

Change (in `TestResolveBaseURL_SettingsFallback`):
```go
if got := resolveBaseURL(settings); got != "settings-url" {
```
to:
```go
if got := resolveBaseURL(settings, detect.Profile{}); got != "settings-url" {
```

Append a new test proving the prefix path (and future-CLI extensibility):
```go
func TestResolveBaseURL_UsesCLIPrefix(t *testing.T) {
	t.Setenv("CERBERUS_LLM_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "openai-url")
	t.Setenv("ANTHROPIC_BASE_URL", "anthropic-url")
	got := resolveBaseURL(nil, detect.Profile{EnvPrefix: "OPENAI"})
	if got != "openai-url" {
		t.Errorf("resolveBaseURL() = %q, want openai-url (CLI prefix wins)", got)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestResolveBaseURL -v`
Expected: FAIL — compile error `too many arguments in call to resolveBaseURL`.

- [x] **Step 3: Update `resolveBaseURL` and its call site**

In `internal/config/config.go`, add the import:
```go
import (
	"os"

	"github.com/binoctal/cerberus/internal/detect"
)
```

Replace the `resolveBaseURL` function (current lines 46-59) with:
```go
// resolveBaseURL picks the base URL: explicit CERBERUS override, then the
// detected host CLI's credential prefix, then the historical ANTHROPIC default
// (so unknown CLIs are unchanged). Environment beats settings.json at each tier.
func resolveBaseURL(settings map[string]string, p detect.Profile) string {
	if v := os.Getenv("CERBERUS_LLM_BASE_URL"); v != "" {
		return v
	}
	if p.EnvPrefix != "" {
		if v := os.Getenv(p.EnvPrefix + "_BASE_URL"); v != "" {
			return v
		}
		if v := settings[p.EnvPrefix+"_BASE_URL"]; v != "" {
			return v
		}
	}
	// Graceful fallback: historical anthropic default for unknown CLIs.
	if v := os.Getenv("ANTHROPIC_BASE_URL"); v != "" {
		return v
	}
	if v := settings["ANTHROPIC_BASE_URL"]; v != "" {
		return v
	}
	return ""
}
```

Update the `Load()` call site (currently `LLMBaseURL: resolveBaseURL(settings),`) to pass an unknown profile for now (detection wiring lands in Task 4):
```go
		LLMBaseURL:   resolveBaseURL(settings, detect.Profile{}),
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — all existing config tests + the new `TestResolveBaseURL_UsesCLIPrefix`.

- [x] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/settings_test.go
git commit -m "refactor(config): thread Profile into resolveBaseURL"
```

---

### Task 3: Parameterize `resolveAPIKey` by `Profile`

**Files:**
- Modify: `internal/config/config.go` (`resolveAPIKey` signature; new `providerKey` helper; `Load` call site)
- Modify: `internal/config/settings_test.go` (update 3 calls; add prefix test)
- Modify: `internal/config/config_test.go` (no logic change; confirm `TestAPIKeyAutoDetect` still passes)

- [x] **Step 1: Update the resolver tests (signature change fails to compile)**

In `internal/config/settings_test.go`, update the three `resolveAPIKey` calls:

In `TestResolveAPIKey_EnvAuthTokenBeatsSettings`:
```go
if got := resolveAPIKey("glm-5.1", settings); got != "auth-tok" {
```
to:
```go
if got := resolveAPIKey("glm-5.1", settings, detect.Profile{}); got != "auth-tok" {
```

In `TestResolveAPIKey_SettingsFallback`:
```go
if got := resolveAPIKey("glm-5.1", settings); got != "settings-tok" {
```
to:
```go
if got := resolveAPIKey("glm-5.1", settings, detect.Profile{}); got != "settings-tok" {
```

In `TestResolveAPIKey_ExplicitOverrideWins`:
```go
if got := resolveAPIKey("glm-5.1", nil); got != "explicit" {
```
to:
```go
if got := resolveAPIKey("glm-5.1", nil, detect.Profile{}); got != "explicit" {
```

Append a prefix-path test:
```go
func TestResolveAPIKey_UsesCLIPrefix(t *testing.T) {
	t.Setenv("CERBERUS_LLM_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	t.Setenv("OPENAI_API_KEY", "openai-key")
	got := resolveAPIKey("glm-5.1", nil, detect.Profile{EnvPrefix: "OPENAI"})
	if got != "openai-key" {
		t.Errorf("resolveAPIKey() = %q, want openai-key (CLI prefix wins over model name)", got)
	}
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestResolveAPIKey -v`
Expected: FAIL — compile error `too many arguments in call to resolveAPIKey`.

- [x] **Step 3: Update `resolveAPIKey`, add `providerKey`, update the call site**

In `internal/config/config.go`, replace the `resolveAPIKey` function (current lines 61-94) with:
```go
// resolveAPIKey finds the API key for the configured LLM.
// Priority: CERBERUS_LLM_API_KEY > detected CLI's credential prefix
// (env, then settings.json) > model-name inference (env only). Model-name
// inference is retained as a graceful fallback so unknown CLIs — and models
// whose provider differs from the host CLI's — keep working.
func resolveAPIKey(model string, settings map[string]string, p detect.Profile) string {
	if key := os.Getenv("CERBERUS_LLM_API_KEY"); key != "" {
		return key
	}
	if p.EnvPrefix != "" {
		if key := providerKey(p.EnvPrefix, settings); key != "" {
			return key
		}
	}
	switch {
	case isModel(model, "gpt"):
		return os.Getenv("OPENAI_API_KEY")
	case isModel(model, "gemini"):
		return os.Getenv("GEMINI_API_KEY")
	default:
		return providerKey("ANTHROPIC", settings)
	}
}

// providerKey returns the first non-empty credential for an env prefix,
// checking environment then settings.json. AUTH_TOKEN is Anthropic-specific but
// harmless to check for other prefixes (always unset, skipped).
func providerKey(prefix string, settings map[string]string) string {
	if key := os.Getenv(prefix + "_API_KEY"); key != "" {
		return key
	}
	if key := os.Getenv(prefix + "_AUTH_TOKEN"); key != "" {
		return key
	}
	if key := settings[prefix+"_API_KEY"]; key != "" {
		return key
	}
	if key := settings[prefix+"_AUTH_TOKEN"]; key != "" {
		return key
	}
	return ""
}
```

Update the `Load()` call site (currently `cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel, settings)`) to:
```go
	cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel, settings, detect.Profile{})
```

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — including `TestAPIKeyAutoDetect` (gpt model still resolves the `OPENAI_API_KEY` via the model-name fallback even when run under Claude Code) and the new `TestResolveAPIKey_UsesCLIPrefix`.

- [x] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/settings_test.go
git commit -m "refactor(config): thread Profile into resolveAPIKey"
```

---

### Task 4: Wire detection into `config.Load` (provider priority)

**Files:**
- Modify: `internal/config/config.go` (`Config.CLIProfile` field; `Load()` detection + priority; real profile passed to resolvers)
- Modify: `internal/config/config_test.go` (add priority tests)

- [x] **Step 1: Write the failing tests**

Append to `internal/config/config_test.go`:
```go
func TestLoad_ProviderFromDetectedCLI(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CERBERUS_LLM_PROVIDER", "")
	t.Setenv("CERBERUS_NO_CLAUDE_SETTINGS", "1")
	cfg := Load()
	assert.Equal(t, "anthropic", cfg.LLMProvider, "detected Claude Code implies anthropic provider")
	assert.Equal(t, "claude-code", string(cfg.CLIProfile.CLI))
}

func TestLoad_ExplicitProviderBeatsDetection(t *testing.T) {
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CERBERUS_LLM_PROVIDER", "openai")
	cfg := Load()
	assert.Equal(t, "openai", cfg.LLMProvider, "explicit CERBERUS_LLM_PROVIDER overrides detection")
}

func TestLoad_UnknownCLIProviderEmpty(t *testing.T) {
	t.Setenv("CLAUDECODE", "")
	t.Setenv("CERBERUS_LLM_PROVIDER", "")
	t.Setenv("CERBERUS_NO_CLAUDE_SETTINGS", "1")
	cfg := Load()
	assert.Equal(t, "", cfg.LLMProvider, "unknown CLI leaves provider empty so llm falls back to model detection")
	assert.Equal(t, "unknown", string(cfg.CLIProfile.CLI))
}
```

- [x] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestLoad_ProviderFromDetectedCLI|TestLoad_ExplicitProviderBeatsDetection|TestLoad_UnknownCLIProviderEmpty' -v`
Expected: FAIL — `cfg.CLIProfile` undefined; `LLMProvider` not yet set from detection.

- [x] **Step 3: Wire detection and provider priority into `Load`**

In `internal/config/config.go`, add the field to the `Config` struct (after `LLMProvider`):
```go
	LLMProvider  string // optional: "anthropic"|"openai"|"gemini"|"mock"; overrides model-based detection
	CLIProfile   detect.Profile // detected host CLI (Unknown when not under a known CLI)
```

Replace the `Load()` function (current lines 16-32) with:
```go
func Load() *Config {
	settings := loadClaudeCodeEnv()
	profile := detect.Detect()

	// Provider priority: explicit env override, then the detected host CLI,
	// then empty (llm.NewClientWithConfig falls back to model-name detection).
	provider := os.Getenv("CERBERUS_LLM_PROVIDER")
	if provider == "" {
		provider = profile.Provider
	}

	cfg := &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBPath:       getEnv("CERBERUS_DB_PATH", "cerberus.db"),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     resolveModel(settings),
		LLMBaseURL:   resolveBaseURL(settings, profile),
		LLMProvider:  provider,
		CLIProfile:   profile,
	}

	// API key resolution: explicit CERBERUS key first, then CLI prefix, then
	// model-name inference (see resolveAPIKey).
	cfg.LLMAPIKey = resolveAPIKey(cfg.LLMModel, settings, profile)

	return cfg
}
```

Note: this replaces the placeholder `detect.Profile{}` args from Tasks 2-3 with the real `profile`.

- [x] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS — all config tests green, including the three new priority tests.

- [x] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): resolve LLM provider from detected host CLI"
```

---

### Task 5: Verify llm trusts Provider + full check

**Files:**
- Verify only (no edit unless a test is missing). `internal/llm/client_test.go` already has `TestNewClientWithConfig_ProviderOverride`.

- [x] **Step 1: Confirm the llm provider-override regression passes**

Run: `go test ./internal/llm/ -run TestNewClientWithConfig_ProviderOverride -v`
Expected: PASS — `Provider=mock` overrides model-based detection for `glm-5.1`. This is the contract that lets the detected provider flow from `config.Load` → `cfg.LLMProvider` → `NewClientWithConfig` without `internal/llm` ever looking at the model name.

- [x] **Step 2: Run the full check (fmt + lint + test)**

Run: `make check`
Expected: PASS — `gofmt`/`goimports` clean, `golangci-lint` clean, all tests pass with `-race`.

- [x] **Step 3: Smoke-test under Claude Code (manual)**

```bash
make build
CLAUDECODE=1 ./bin/cerberus version   # confirm binary runs under the detected CLI env
```
Expected: no error. (Full `cerberus run` end-to-end is optional; the dogfood run from the prior session already proved the binding works — detection now makes the *reason* deterministic rather than a model-prefix coincidence.)

- [x] **Step 4: Commit (only if Step 1/2 added anything; otherwise skip)**

If no files changed, this task needs no commit. If a regression test was added, commit it:
```bash
git add internal/llm/
git commit -m "test(llm): lock provider-override contract for CLI-driven provider"
```

---

## Self-Review (completed during planning)

- **Spec coverage:** §1 detect package → Task 1. §2 config.Load integration → Tasks 2-4 (resolvers prefix-aware + Load detection). §3 provider priority → Task 4 (`CERBERUS_LLM_PROVIDER` > detected > empty-→-llm-model-fallback). §4 llm trusts Provider → Task 5 (already implemented; regression locked). §5 testing → every task is TDD. §6 extensibility → Task 1 registry (`detectors` slice) + prefix tests in Tasks 2-3 prove a second CLI works by prefix alone.
- **Placeholder scan:** none — every code step shows full code; every command shows expected output.
- **Type consistency:** `detect.Profile` used consistently; `providerKey` defined in Task 3 before use; `Config.CLIProfile` field type matches `detect.Profile`; resolver signatures match across definition (Tasks 2/3) and call sites (Task 4). `resolveModel` deliberately kept single-arg (no prefix benefit today).
- **Behavioral preservation:** unknown-CLI paths fall through to the historical `ANTHROPIC` defaults and model-prefix switches, so `TestLoadDefaults`, `TestAPIKeyAutoDetect`, and all `settings_test.go` resolver tests pass unchanged in behavior (only call signatures updated).

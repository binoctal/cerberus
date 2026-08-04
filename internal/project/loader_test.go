package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromFile_NoEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
services:
  - name: api
    url: "http://localhost:8080"
settings:
  confidence_threshold: 0.8
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "my-app", cfg.Project.Name)
	assert.Equal(t, "http://localhost:8080", cfg.Services[0].URL)
	assert.InDelta(t, 0.8, cfg.Settings.ConfidenceThreshold, 0.01)
}

func TestLoadFromFile_EnvOverlay(t *testing.T) {
	dir := t.TempDir()

	// Base config.
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
services:
  - name: api
    url: "http://localhost:8080"
settings:
  confidence_threshold: 0.8
  ai_budget:
    session_total_tokens: 200000
    model: claude-sonnet-4-6
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	// Staging overlay: only override what changes.
	overlayYAML := `
services:
  - name: api
    url: "https://staging.my-app.io"
settings:
  confidence_threshold: 0.9
  ai_budget:
    session_total_tokens: 400000
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.staging.yaml"), []byte(overlayYAML), 0644))

	t.Setenv("CERBERUS_ENV", "staging")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)

	// Overridden fields.
	assert.Equal(t, "https://staging.my-app.io", cfg.Services[0].URL)
	assert.InDelta(t, 0.9, cfg.Settings.ConfidenceThreshold, 0.01)
	assert.Equal(t, 400000, cfg.Settings.AIBudget.SessionTotalTokens)

	// Base fields preserved.
	assert.Equal(t, "my-app", cfg.Project.Name)
	assert.Equal(t, "claude-sonnet-4-6", cfg.Settings.AIBudget.Model)
}

func TestLoadFromFile_EnvOverlay_MissingFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
settings:
  confidence_threshold: 0.7
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	// CERBERUS_ENV set but no overlay file — should not error.
	t.Setenv("CERBERUS_ENV", "prod")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "my-app", cfg.Project.Name)
}

func TestLoadFromFile_EnvOverlay_EnvInterpolation(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "project.yaml")
	baseYAML := `
project:
  name: my-app
settings:
  confidence_threshold: 0.7
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(baseYAML), 0644))

	overlayYAML := `
services:
  - name: api
    url: "${API_URL}"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "project.staging.yaml"), []byte(overlayYAML), 0644))

	t.Setenv("CERBERUS_ENV", "staging")
	t.Setenv("API_URL", "https://staging.example.com")

	cfg, err := LoadFromFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "https://staging.example.com", cfg.Services[0].URL)
}

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

// TestLoadFromFile_ProtocolRefResolvesAtCerberusConfigLocation guards the
// documented/default config layout: <root>/.cerberus/project.yaml with
// protocols under <root>/.cerberus/protocols/. The config's own directory is
// .cerberus, so protocol_ref must resolve relative to the project root (one
// level up), not the config directory — otherwise the path doubles to
// .cerberus/.cerberus/protocols/.
func TestLoadFromFile_ProtocolRefResolvesAtCerberusConfigLocation(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "project.yaml"), []byte(`
project: { name: app }
services:
  - name: rt
    url: "http://localhost:8787"
    protocol_ref: open-agents
`), 0644))
	writeProtocolFile(t, dir, "open-agents", "framing: json\ntype_path: type\n")

	cfg, err := LoadFromFile(filepath.Join(dir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Protocol)
	assert.Equal(t, "json", cfg.Services[0].Protocol.Framing)
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

func TestResolveProtocolRefsLoadsVocab(t *testing.T) {
	dir := t.TempDir()
	writeProtocolFile(t, dir, "open-agents", "framing: json\ntype_path: type\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus", "vocab"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "vocab", "open-agents.vocab.yaml"),
		[]byte("source:\n  protocol_ref: open-agents\nedges:\n  - from_role: bridge\n    to_role: web\n    type: session:created\n    trigger: message_handled\n    delivery: {mode: broadcast_web}\n    source: {spans: [{start: 1, end: 1}]}\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "project.yaml"),
		[]byte("project:\n  name: t\nservices:\n  - name: rt\n    url: http://localhost:8787\n    protocol_ref: open-agents\n"), 0644))

	cfg, err := LoadFromFile(filepath.Join(dir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg.Services[0].Vocabulary, "Vocabulary not loaded alongside protocol_ref")
	require.Len(t, cfg.Services[0].Vocabulary.Edges, 1)
}

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

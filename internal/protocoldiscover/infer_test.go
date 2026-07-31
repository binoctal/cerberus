package protocoldiscover

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

func TestInfer_ReturnsValidatedProtocol(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "rt", URL: "http://x"}},
		Actors:   []project.Actor{{Name: "web"}},
	}
	driver := mockDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"notes":     "ok",
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", []SourceFile{{Path: "docs.md", Content: "..."}})
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
	assert.Equal(t, "type", p.TypePath)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "web", p.Auth.CredentialRef)
}

func TestInfer_NoProtocol(t *testing.T) {
	driver := mockDriver(t, map[string]any{"found": false})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil)
	assert.ErrorIs(t, err, ErrNoProtocol)
}

func TestInfer_InvalidCredentialRefFailsValidation(t *testing.T) {
	driver := mockDriver(t, map[string]any{
		"found": true, "framing": "json",
		"auth": map[string]any{"strategy": "query", "param": "token", "credential_ref": "ghost"},
	})
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match any actor")
}

// TestInfer_RolesPopulated exercises the roles mapping path. Without the
// p.Roles initialization this would panic on the nil-map write, so this test
// guards the fix for that latent bug (the model returned roles but the brief's
// original code constructed p with a nil Roles map).
func TestInfer_RolesPopulated(t *testing.T) {
	cfg := cfgWithService()
	driver := mockDriver(t, map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth":      map[string]any{"strategy": "query", "param": "token", "credential_ref": "web"},
		"roles": map[string]any{
			"web": map[string]any{
				"credential_ref": "web",
				"params":         map[string]any{"type": "web"},
				"handshake":      map[string]any{"await_type": "ready", "timeout": 5},
			},
		},
	})
	p, err := Infer(context.Background(), driver, cfg, "rt", nil)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Contains(t, p.Roles, "web")
	role := p.Roles["web"]
	assert.Equal(t, "web", role.CredentialRef)
	assert.Equal(t, map[string]string{"type": "web"}, role.Params)
	require.NotNil(t, role.Handshake)
	assert.Equal(t, "ready", role.Handshake.AwaitType)
	assert.Equal(t, 5, role.Handshake.Timeout)
}

// TestInfer_UnparseableHidesRaw mirrors authdiscover's leak guard: a parse
// failure must return a static message and must NOT embed the raw LLM body.
func TestInfer_UnparseableHidesRaw(t *testing.T) {
	driver := mockDriverRaw(t, "not json at all SECRET-MARKER-XYZ")
	_, err := Infer(context.Background(), driver, cfgWithService(), "rt", nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SECRET-MARKER-XYZ")
	assert.False(t, errors.Is(err, ErrNoProtocol), "parse failure must not be reported as ErrNoProtocol")
}

func cfgWithService() *project.Config {
	return &project.Config{
		Services: []project.Service{{Name: "rt", URL: "http://x"}},
		Actors:   []project.Actor{{Name: "web"}},
	}
}

// mockDriver builds an *ai.Driver whose Decide parses the JSON-encoded form of
// out. It mirrors the authdiscover test helper (driverReturning): a canned
// llm.MockClient keyed on "default" wrapped in ai.NewDriver. No real LLM is
// contacted, and no credential values are sent anywhere.
func mockDriver(t *testing.T, out any) *ai.Driver {
	t.Helper()
	raw, err := json.Marshal(out)
	require.NoError(t, err)
	return mockDriverRaw(t, string(raw))
}

// mockDriverRaw wraps an arbitrary canned response (used by the unparseable
// test, which needs non-JSON input).
func mockDriverRaw(t *testing.T, resp string) *ai.Driver {
	t.Helper()
	mock := llm.NewMockClient(map[string]string{"default": resp})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

func TestBuildInferPrompt_RecognitionGuidance(t *testing.T) {
	prompt := buildInferPrompt("rt", []SourceFile{{Path: "room.ts", Content: "..."}})

	// The four in-scope structures must be called out so the model knows to
	// populate the corresponding tool fields.
	for _, want := range []string{"envelope", "batch", "handshake", "role"} {
		assert.Contains(t, prompt, want, "prompt must guide recognition of %q", want)
	}
	// The prompt must no longer hand-write a JSON shape block (the tool schema
	// now carries the shape). The legacy marker was the literal `"found":`.
	assert.NotContains(t, prompt, `"found":`, "prompt must not hand-write the JSON shape; the tool schema owns it")
	// credential_ref safety constraint must remain.
	assert.Contains(t, prompt, "credential_ref")
}

// TestTruncateContent_RuneSafe guards against byte-slicing a multi-byte rune at
// the truncation point: the output must remain valid UTF-8. "世界" is 2 runes /
// 6 bytes; byte-slicing at an odd boundary would split the second rune.
func TestTruncateContent_RuneSafe(t *testing.T) {
	s := strings.Repeat("世界", 10) // 20 runes, 60 bytes
	out := truncateContent(s, 5)
	if !utf8.ValidString(out) {
		t.Errorf("truncated output is not valid UTF-8: %q", out)
	}
	// The rune prefix before the marker must be exactly 5 runes.
	marker := strings.Index(out, "\n…[truncated]")
	if marker < 0 {
		t.Fatalf("missing truncation marker in %q", out)
	}
	prefix := out[:marker]
	if got := utf8.RuneCountInString(prefix); got != 5 {
		t.Errorf("prefix rune count = %d, want 5", got)
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/binoctal/cerberus/internal/detect"
)

func TestFindUp_SearchesUpward(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(".claude", "settings.json")
	want := filepath.Join(root, target)
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findUp(nested, target); got != want {
		t.Errorf("findUp() = %q, want %q", got, want)
	}
}

func TestFindUp_NotFound(t *testing.T) {
	if got := findUp(t.TempDir(), ".claude/settings.json"); got != "" {
		t.Errorf("findUp() = %q, want empty", got)
	}
}

func TestLoadClaudeCodeEnvFrom_ReadsAnthropicConfig(t *testing.T) {
	dir := t.TempDir()
	cls := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(cls), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"env":{"ANTHROPIC_BASE_URL":"https://example.com/api/anthropic","ANTHROPIC_AUTH_TOKEN":"tok-123","ANTHROPIC_DEFAULT_SONNET_MODEL":"glm-5.1"}}`
	if err := os.WriteFile(cls, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	env := loadClaudeCodeEnvFrom(dir)
	if env["ANTHROPIC_BASE_URL"] != "https://example.com/api/anthropic" {
		t.Errorf("base url = %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "tok-123" {
		t.Errorf("auth token = %q", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5.1" {
		t.Errorf("model = %q", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
}

func TestLoadClaudeCodeEnvFrom_MissingFile(t *testing.T) {
	if env := loadClaudeCodeEnvFrom(t.TempDir()); env != nil {
		t.Errorf("want nil for missing file, got %v", env)
	}
}

func TestResolveModel_EnvOverridesSettings(t *testing.T) {
	t.Setenv("CERBERUS_LLM_MODEL", "env-model")
	settings := map[string]string{"ANTHROPIC_DEFAULT_SONNET_MODEL": "settings-model"}
	if got := resolveModel(settings); got != "env-model" {
		t.Errorf("resolveModel() = %q, want env-model", got)
	}
}

func TestResolveModel_SettingsFallback(t *testing.T) {
	t.Setenv("CERBERUS_LLM_MODEL", "")
	settings := map[string]string{"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1"}
	if got := resolveModel(settings); got != "glm-5.1" {
		t.Errorf("resolveModel() = %q, want glm-5.1", got)
	}
}

func TestResolveModel_Default(t *testing.T) {
	t.Setenv("CERBERUS_LLM_MODEL", "")
	if got := resolveModel(nil); got != "claude-sonnet-4-6" {
		t.Errorf("resolveModel() = %q, want claude-sonnet-4-6", got)
	}
}

func TestResolveBaseURL_Priority(t *testing.T) {
	t.Setenv("CERBERUS_LLM_BASE_URL", "")
	t.Setenv("ANTHROPIC_BASE_URL", "env-url")
	settings := map[string]string{"ANTHROPIC_BASE_URL": "settings-url"}
	if got := resolveBaseURL(settings, detect.Profile{}); got != "env-url" {
		t.Errorf("resolveBaseURL() = %q, want env-url", got)
	}
}

func TestResolveBaseURL_SettingsFallback(t *testing.T) {
	t.Setenv("CERBERUS_LLM_BASE_URL", "")
	t.Setenv("ANTHROPIC_BASE_URL", "")
	settings := map[string]string{"ANTHROPIC_BASE_URL": "settings-url"}
	if got := resolveBaseURL(settings, detect.Profile{}); got != "settings-url" {
		t.Errorf("resolveBaseURL() = %q, want settings-url", got)
	}
}

func TestResolveBaseURL_UsesCLIPrefix(t *testing.T) {
	t.Setenv("CERBERUS_LLM_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "openai-url")
	t.Setenv("ANTHROPIC_BASE_URL", "anthropic-url")
	got := resolveBaseURL(nil, detect.Profile{EnvPrefix: "OPENAI"})
	if got != "openai-url" {
		t.Errorf("resolveBaseURL() = %q, want openai-url (CLI prefix wins)", got)
	}
}

func TestResolveAPIKey_EnvAuthTokenBeatsSettings(t *testing.T) {
	t.Setenv("CERBERUS_LLM_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "auth-tok")
	settings := map[string]string{"ANTHROPIC_API_KEY": "settings-key"}
	if got := resolveAPIKey("glm-5.1", settings, detect.Profile{}); got != "auth-tok" {
		t.Errorf("resolveAPIKey() = %q, want auth-tok (env AUTH_TOKEN beats settings API_KEY)", got)
	}
}

func TestResolveAPIKey_SettingsFallback(t *testing.T) {
	t.Setenv("CERBERUS_LLM_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "")
	settings := map[string]string{"ANTHROPIC_AUTH_TOKEN": "settings-tok"}
	if got := resolveAPIKey("glm-5.1", settings, detect.Profile{}); got != "settings-tok" {
		t.Errorf("resolveAPIKey() = %q, want settings-tok", got)
	}
}

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

func TestResolveAPIKey_ExplicitOverrideWins(t *testing.T) {
	t.Setenv("CERBERUS_LLM_API_KEY", "explicit")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "auth-tok")
	if got := resolveAPIKey("glm-5.1", nil, detect.Profile{}); got != "explicit" {
		t.Errorf("resolveAPIKey() = %q, want explicit", got)
	}
}

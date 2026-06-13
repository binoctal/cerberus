package config

import (
	"testing"

	"github.com/binoctal/cerberus/internal/detect"
	"github.com/stretchr/testify/assert"
)

func TestResolveTierModels_ClaudeCode_AssignsByComplexity(t *testing.T) {
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-4-flash",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-5.2",
	}
	got := resolveTierModels(detect.CLIClaudeCode, settings)
	assert.Equal(t, "glm-4-flash", got[HeadAgent], "Agent runs on the fast tier")
	assert.Equal(t, "glm-5.1", got[HeadScout], "Scout plans on the mid tier")
	assert.Equal(t, "glm-5.1", got[HeadExaminer], "Examiner judges on the mid tier")
	assert.Equal(t, "glm-5.2", got[HeadCritic], "Critic reviews on the strong tier")
}

func TestResolveTierModels_UnknownCLI_Empty(t *testing.T) {
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
	}
	got := resolveTierModels(detect.CLIUnknown, settings)
	assert.Empty(t, got, "CLIUnknown leaves tier resolution to the existing logic")
}

func TestResolveTierModels_MissingTierLeavesEmpty(t *testing.T) {
	// Only SONNET set; HAIKU/OPUS unset → Agent/Critic map to "" (caller falls back).
	settings := map[string]string{
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.1",
	}
	got := resolveTierModels(detect.CLIClaudeCode, settings)
	assert.Equal(t, "glm-5.1", got[HeadScout])
	assert.Equal(t, "", got[HeadAgent], "missing HAIKU tier → Agent falls back to global")
	assert.Equal(t, "", got[HeadCritic], "missing OPUS tier → Critic falls back to global")
}

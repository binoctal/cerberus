package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/detect"
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

func TestPickModel_PriorityChain(t *testing.T) {
	tier := TierModels{
		HeadAgent:    "tier-haiku",
		HeadScout:    "tier-sonnet",
		HeadExaminer: "tier-sonnet",
		// HeadCritic intentionally absent from tier.
	}
	tests := []struct {
		name     string
		head     Head
		explicit string
		global   string
		want     string
	}{
		{"explicit wins over tier and global", HeadAgent, "explicit-m", "global-m", "explicit-m"},
		{"explicit wins even when tier present", HeadScout, "explicit-m", "global-m", "explicit-m"},
		{"tier used when no explicit", HeadAgent, "", "global-m", "tier-haiku"},
		{"global used when no explicit and head absent from tier", HeadCritic, "", "global-m", "global-m"},
		{"empty when nothing resolves", HeadCritic, "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PickModel(tc.head, tc.explicit, tier, tc.global)
			assert.Equal(t, tc.want, got)
		})
	}
}

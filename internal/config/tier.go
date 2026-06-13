package config

import "github.com/binoctal/cerberus/internal/detect"

// Head identifies a cerberus head that consumes its own LLM driver.
type Head string

const (
	HeadScout    Head = "scout"
	HeadAgent    Head = "agent"
	HeadExaminer Head = "examiner"
	HeadCritic   Head = "critic"
)

// TierModels maps each head to a model selected from the host CLI's tier envs.
// A head mapped to "" has no tier assignment; the caller applies the global
// model / built-in default fallback.
type TierModels map[Head]string

// resolveTierModels assigns each head a model tier by task complexity. Tiers
// are read from the settings map that settings.go populates from the host CLI's
// env block. Only Claude Code declares these tiers today; any other CLI yields
// an empty map so the existing sonnet-only resolution is preserved.
func resolveTierModels(cli detect.CLI, settings map[string]string) TierModels {
	if cli != detect.CLIClaudeCode {
		return TierModels{}
	}
	haiku := settings["ANTHROPIC_DEFAULT_HAIKU_MODEL"]
	sonnet := settings["ANTHROPIC_DEFAULT_SONNET_MODEL"]
	opus := settings["ANTHROPIC_DEFAULT_OPUS_MODEL"]
	return TierModels{
		// Execution is frequent and mechanical → fast tier.
		HeadAgent: haiku,
		// Planning and judgment carry quality weight → mid tier.
		HeadScout:    sonnet,
		HeadExaminer: sonnet,
		// Low-frequency high-stakes review → strong tier.
		HeadCritic: opus,
	}
}

package config

import (
	"github.com/binoctal/cerberus/internal/detect"
	"github.com/binoctal/cerberus/internal/llm"
)

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

// TierContexts maps each head to its model's input-token capacity, looked up
// from the llm registry. A head with no resolved model has context 0.
type TierContexts map[Head]int

// resolveTierContexts looks up each head's tier model in the llm registry to
// capture its context window. Used to scale ToT/Reflexion depth to what the
// model can actually hold.
func resolveTierContexts(tiers TierModels) TierContexts {
	ctxs := TierContexts{}
	for head, model := range tiers {
		if model != "" {
			ctxs[head] = llm.ContextWindow(model)
		}
	}
	return ctxs
}

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

// PickModel resolves a head's model by the Phase 1 priority chain:
// explicit settings.models override > tier assigned by the host CLI >
// global ai_budget.model. Returns "" when nothing resolves, in which case
// the caller falls back to the shared Driver.
func PickModel(head Head, explicit string, tier TierModels, global string) string {
	if explicit != "" {
		return explicit
	}
	if m, ok := tier[head]; ok && m != "" {
		return m
	}
	return global
}

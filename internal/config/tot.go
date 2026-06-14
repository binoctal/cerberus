package config

import (
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/project"
)

// ResolveToTConfig maps project.yaml settings.tot onto a ToTConfig, scaled to
// the Scout model's context window. Any unset (zero) field falls back to the
// context-scaled default (depthForContextToT), so omitting the block still
// adapts depth to the model: 1M-class models get a deeper search, small-context
// models get a conservative one. Explicit settings.tot fields always win.
func ResolveToTConfig(s project.Settings, contextTokens int) scout.ToTConfig {
	cfg := depthForContextToT(contextTokens)
	if s.ToT.BeamWidth > 0 {
		cfg.BeamWidth = s.ToT.BeamWidth
	}
	if s.ToT.GenerateN > 0 {
		cfg.GenerateN = s.ToT.GenerateN
	}
	if s.ToT.MaxSteps > 0 {
		cfg.MaxSteps = s.ToT.MaxSteps
	}
	return cfg
}

// depthForContextToT returns context-scaled ToT defaults:
//   - ≥500K (1M-class):   beam 5, generate 8, steps 5 — deep search
//   - ≥128K (200K-class): DefaultToTConfig — beam 3, generate 5, steps 3
//   - <128K / unknown:    beam 2, generate 3, steps 2 — conservative
func depthForContextToT(contextTokens int) scout.ToTConfig {
	switch {
	case contextTokens >= 500_000:
		return scout.ToTConfig{BeamWidth: 5, GenerateN: 8, MaxSteps: 5}
	case contextTokens >= 128_000:
		return scout.DefaultToTConfig()
	default:
		return scout.ToTConfig{BeamWidth: 2, GenerateN: 3, MaxSteps: 2}
	}
}

// ResolveReflexionConfig maps settings.reflexion onto ReflexionSettings, scaled
// to the Scout model's context window. Unset fields keep the context-scaled
// default.
func ResolveReflexionConfig(s project.Settings, contextTokens int) project.ReflexionSettings {
	r := depthForContextReflexion(contextTokens)
	if s.Reflexion.EpisodicLimit > 0 {
		r.EpisodicLimit = s.Reflexion.EpisodicLimit
	}
	if s.Reflexion.SemanticTopK > 0 {
		r.SemanticTopK = s.Reflexion.SemanticTopK
	}
	if s.Reflexion.SemanticThreshold > 0 {
		r.SemanticThreshold = s.Reflexion.SemanticThreshold
	}
	return r
}

// depthForContextReflexion returns context-scaled memory-recall defaults:
//   - ≥500K: episodic 20, semantic 10
//   - ≥128K: episodic 10, semantic 5 (prior hardcoded defaults)
//   - <128K: episodic 5, semantic 3
func depthForContextReflexion(contextTokens int) project.ReflexionSettings {
	switch {
	case contextTokens >= 500_000:
		return project.ReflexionSettings{EpisodicLimit: 20, SemanticTopK: 10, SemanticThreshold: 0.3}
	case contextTokens >= 128_000:
		return project.ReflexionSettings{EpisodicLimit: 10, SemanticTopK: 5, SemanticThreshold: 0.3}
	default:
		return project.ReflexionSettings{EpisodicLimit: 5, SemanticTopK: 3, SemanticThreshold: 0.3}
	}
}

package config

import (
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/project"
)

// ResolveToTConfig maps the project.yaml settings.tot block onto a ToTConfig.
// Any unset (zero) field falls back to DefaultToTConfig (beam_width 3,
// generate_n 5, max_steps 3), so omitting the block entirely preserves the
// prior hardcoded behavior.
func ResolveToTConfig(s project.Settings) scout.ToTConfig {
	cfg := scout.DefaultToTConfig()
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

// ResolveReflexionConfig maps settings.reflexion onto ReflexionSettings with
// defaults (episodic_limit 10, semantic_topk 5, semantic_threshold 0.3). Unset
// fields keep their defaults.
func ResolveReflexionConfig(s project.Settings) project.ReflexionSettings {
	r := project.ReflexionSettings{
		EpisodicLimit:     10,
		SemanticTopK:      5,
		SemanticThreshold: 0.3,
	}
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

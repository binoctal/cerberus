package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/project"
)

func TestResolveToTConfig(t *testing.T) {
	// Empty settings → all defaults (3/5/3).
	assert.Equal(t, scout.ToTConfig{BeamWidth: 3, GenerateN: 5, MaxSteps: 3},
		ResolveToTConfig(project.Settings{}))

	// Full override.
	got := ResolveToTConfig(project.Settings{ToT: project.ToTSettings{BeamWidth: 7, GenerateN: 2, MaxSteps: 9}})
	assert.Equal(t, scout.ToTConfig{BeamWidth: 7, GenerateN: 2, MaxSteps: 9}, got)

	// Partial override keeps defaults for unset (zero) fields.
	got = ResolveToTConfig(project.Settings{ToT: project.ToTSettings{MaxSteps: 4}})
	assert.Equal(t, 3, got.BeamWidth) // default
	assert.Equal(t, 5, got.GenerateN) // default
	assert.Equal(t, 4, got.MaxSteps)  // override
}

func TestResolveReflexionConfig(t *testing.T) {
	// Empty → defaults (episodic_limit 10, semantic_topk 5, semantic_threshold 0.3).
	assert.Equal(t,
		project.ReflexionSettings{EpisodicLimit: 10, SemanticTopK: 5, SemanticThreshold: 0.3},
		ResolveReflexionConfig(project.Settings{}))

	got := ResolveReflexionConfig(project.Settings{Reflexion: project.ReflexionSettings{EpisodicLimit: 20, SemanticThreshold: 0.5}})
	assert.Equal(t, 20, got.EpisodicLimit)
	assert.Equal(t, 5, got.SemanticTopK) // default
	assert.Equal(t, 0.5, got.SemanticThreshold)
}

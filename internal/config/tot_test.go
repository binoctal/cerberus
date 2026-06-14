package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/project"
)

func TestResolveToTConfig(t *testing.T) {
	// Empty settings at 200K-class context → DefaultToTConfig (3/5/3).
	assert.Equal(t, scout.ToTConfig{BeamWidth: 3, GenerateN: 5, MaxSteps: 3},
		ResolveToTConfig(project.Settings{}, 200_000))
	// Explicit override wins regardless of context.
	got := ResolveToTConfig(project.Settings{ToT: project.ToTSettings{BeamWidth: 7, GenerateN: 2, MaxSteps: 9}}, 1_000_000)
	assert.Equal(t, scout.ToTConfig{BeamWidth: 7, GenerateN: 2, MaxSteps: 9}, got)
	// Partial override keeps the context default for unset fields.
	got = ResolveToTConfig(project.Settings{ToT: project.ToTSettings{MaxSteps: 4}}, 200_000)
	assert.Equal(t, 3, got.BeamWidth) // context default
	assert.Equal(t, 5, got.GenerateN) // context default
	assert.Equal(t, 4, got.MaxSteps)  // override
}

func TestDepthForContextToT(t *testing.T) {
	assert.Equal(t, scout.ToTConfig{BeamWidth: 5, GenerateN: 8, MaxSteps: 5}, depthForContextToT(1_000_000))
	assert.Equal(t, scout.ToTConfig{BeamWidth: 5, GenerateN: 8, MaxSteps: 5}, depthForContextToT(500_000))
	assert.Equal(t, scout.DefaultToTConfig(), depthForContextToT(200_000))
	assert.Equal(t, scout.DefaultToTConfig(), depthForContextToT(128_000))
	assert.Equal(t, scout.ToTConfig{BeamWidth: 2, GenerateN: 3, MaxSteps: 2}, depthForContextToT(64_000))
	assert.Equal(t, scout.ToTConfig{BeamWidth: 2, GenerateN: 3, MaxSteps: 2}, depthForContextToT(0)) // unknown/standalone
}

func TestResolveReflexionConfig(t *testing.T) {
	assert.Equal(t,
		project.ReflexionSettings{EpisodicLimit: 10, SemanticTopK: 5, SemanticThreshold: 0.3},
		ResolveReflexionConfig(project.Settings{}, 200_000))
	got := ResolveReflexionConfig(project.Settings{Reflexion: project.ReflexionSettings{EpisodicLimit: 20, SemanticThreshold: 0.5}}, 1_000_000)
	assert.Equal(t, 20, got.EpisodicLimit)
	assert.Equal(t, 10, got.SemanticTopK) // 1M-class default
	assert.Equal(t, 0.5, got.SemanticThreshold)
}

func TestDepthForContextReflexion(t *testing.T) {
	assert.Equal(t, project.ReflexionSettings{EpisodicLimit: 20, SemanticTopK: 10, SemanticThreshold: 0.3}, depthForContextReflexion(1_000_000))
	assert.Equal(t, project.ReflexionSettings{EpisodicLimit: 10, SemanticTopK: 5, SemanticThreshold: 0.3}, depthForContextReflexion(200_000))
	assert.Equal(t, project.ReflexionSettings{EpisodicLimit: 5, SemanticTopK: 3, SemanticThreshold: 0.3}, depthForContextReflexion(64_000))
	assert.Equal(t, project.ReflexionSettings{EpisodicLimit: 5, SemanticTopK: 3, SemanticThreshold: 0.3}, depthForContextReflexion(0))
}

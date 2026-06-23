package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

func TestDiscoverCmd_WritesMergedProjectYAML(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(`
services:
  gateway:
    image: relay-gateway:dev
    ports: ["8081:8080"]
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/health"]
  postgres:
    image: postgres:16
    ports: ["5432:5432"]
`), 0644))

	err := runDiscover(dir, "docker-compose.yml", nil, nil, false)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, ".cerberus", "project.yaml"))
	require.NoError(t, err)
	var cfg project.Config
	require.NoError(t, yaml.Unmarshal(data, &cfg))
	require.Len(t, cfg.Services, 1, "should have exactly one non-infra service")
	assert.Equal(t, "gateway", cfg.Services[0].Name)
}

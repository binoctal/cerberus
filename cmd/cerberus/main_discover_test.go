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

func TestDiscoverCmd_ValidationErrorDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".cerberus", "project.yaml")

	// Create an existing project.yaml with custom content
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), 0755))
	originalContent := `services:
  - name: custom-service
    url: http://localhost:9000
actors:
  - name: test-actor
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(originalContent), 0644))

	// Create docker-compose.yml
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(`
services:
  gateway:
    image: relay-gateway:dev
    ports: ["8081:8080"]
`), 0644))

	// Manually corrupt the project.yaml with duplicate service names to trigger validation error
	corruptedContent := `services:
  - name: duplicate
    url: http://localhost:9000
  - name: duplicate
    url: http://localhost:9001
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(corruptedContent), 0644))

	// runDiscover should fail due to validation error
	err := runDiscover(dir, "docker-compose.yml", nil, nil, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "load existing project.yaml")

	// File content should remain unchanged (corrupted)
	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, corruptedContent, string(data))
}

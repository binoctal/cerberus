package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad(t *testing.T) {
	os.Setenv("CERBERUS_PORT", "9090")
	defer os.Unsetenv("CERBERUS_PORT")

	cfg := Load()
	assert.Equal(t, "9090", cfg.Port)
	assert.Equal(t, "5432", cfg.DBPort) // default
}

func TestLoadDefaults(t *testing.T) {
	for _, key := range []string{
		"CERBERUS_PORT", "CERBERUS_DB_HOST", "CERBERUS_DB_PORT",
		"CERBERUS_DB_USER", "CERBERUS_DB_PASSWORD", "CERBERUS_DB_NAME",
		"CERBERUS_MIGRATION_DIR", "CERBERUS_LOG_LEVEL", "CERBERUS_LLM_MODEL",
	} {
		os.Unsetenv(key)
	}

	cfg := Load()
	assert.Equal(t, "8090", cfg.Port)
	assert.Equal(t, "localhost", cfg.DBHost)
	assert.Equal(t, "5432", cfg.DBPort)
	assert.Equal(t, "cerberus", cfg.DBUser)
	assert.Equal(t, "cerberus", cfg.DBName)
	assert.Equal(t, "migrations", cfg.MigrationDir)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "claude-sonnet-4-6", cfg.LLMModel)
}

func TestDBURL(t *testing.T) {
	cfg := &Config{
		DBHost:     "localhost",
		DBPort:     "5432",
		DBUser:     "test",
		DBPassword: "secret",
		DBName:     "cerberus_test",
	}
	expected := "postgres://test:secret@localhost:5432/cerberus_test?sslmode=disable"
	assert.Equal(t, expected, cfg.DBURL())
}

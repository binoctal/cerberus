package config

import "os"

type Config struct {
	Port         string
	DBHost       string
	DBPort       string
	DBUser       string
	DBPassword   string
	DBName       string
	MigrationDir string
	LogLevel     string
	LLMModel     string
	LLMAPIKey    string
}

func Load() *Config {
	return &Config{
		Port:         getEnv("CERBERUS_PORT", "8090"),
		DBHost:       getEnv("CERBERUS_DB_HOST", "localhost"),
		DBPort:       getEnv("CERBERUS_DB_PORT", "5432"),
		DBUser:       getEnv("CERBERUS_DB_USER", "cerberus"),
		DBPassword:   getEnv("CERBERUS_DB_PASSWORD", "cerberus"),
		DBName:       getEnv("CERBERUS_DB_NAME", "cerberus"),
		MigrationDir: getEnv("CERBERUS_MIGRATION_DIR", "migrations"),
		LogLevel:     getEnv("CERBERUS_LOG_LEVEL", "info"),
		LLMModel:     getEnv("CERBERUS_LLM_MODEL", "claude-sonnet-4-6"),
		LLMAPIKey:    getEnv("CERBERUS_LLM_API_KEY", ""),
	}
}

func (c *Config) DBURL() string {
	return "postgres://" + c.DBUser + ":" + c.DBPassword + "@" +
		c.DBHost + ":" + c.DBPort + "/" + c.DBName + "?sslmode=disable"
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

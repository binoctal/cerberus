package project

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

var envVarRE = regexp.MustCompile(`\$\{([^}]+)\}`)

func LoadFromYAML(data []byte) (*Config, error) {
	interpolated := envVarRE.ReplaceAllFunc(data, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1])
		if val := os.Getenv(varName); val != "" {
			return []byte(val)
		}
		return match
	})

	var cfg Config
	if err := yaml.Unmarshal(interpolated, &cfg); err != nil {
		return nil, fmt.Errorf("parse project config: %w", err)
	}

	applyDefaults(&cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project config: %w", err)
	}
	return LoadFromYAML(data)
}

func applyDefaults(cfg *Config) {
	d := DefaultConfig()
	if cfg.Settings.ConfidenceThreshold == 0 {
		cfg.Settings.ConfidenceThreshold = d.Settings.ConfidenceThreshold
	}
	if cfg.Settings.MaxDuration == "" {
		cfg.Settings.MaxDuration = d.Settings.MaxDuration
	}
	if cfg.Settings.AutoFix == "" {
		cfg.Settings.AutoFix = d.Settings.AutoFix
	}
	if cfg.Settings.AIBudget.SessionTotalTokens == 0 {
		cfg.Settings.AIBudget = d.Settings.AIBudget
	}
	if cfg.Settings.CostAlerts.WarnAtPct == 0 {
		cfg.Settings.CostAlerts = d.Settings.CostAlerts
	}
}

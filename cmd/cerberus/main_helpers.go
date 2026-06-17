package main

import (
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
)

// loadProjectConfig loads project configuration from file or defaults
func loadProjectConfig(configPath, url, goal string, logger *zap.Logger) *project.Config {
	var cfg *project.Config
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			loaded, err := project.LoadFromFile(configPath)
			if err != nil {
				logger.Warn("failed to load project config, using defaults", zap.Error(err))
			} else {
				cfg = loaded
			}
		}
	}
	if cfg == nil {
		d := project.DefaultConfig()
		cfg = &d
	}

	// If --url was provided and config has no services, create a synthetic one.
	if url != "" && len(cfg.Services) == 0 {
		cfg.Services = append(cfg.Services, project.Service{
			Name: "default",
			URL:  url,
		})
	}

	return cfg
}

// containsLine checks if content contains a specific line
func containsLine(content, line string) bool {
	return strings.Contains(content, line)
}

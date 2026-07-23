package project

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"dario.cat/mergo"
	"gopkg.in/yaml.v3"
)

var envVarRE = regexp.MustCompile(`\$\{([^}]+)\}`)

func LoadFromYAML(data []byte, baseDir string) (*Config, error) {
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
	if err := resolveProtocolRefs(&cfg, baseDir); err != nil {
		return nil, err
	}
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

	// baseDir for protocol_ref resolution is the project root: the directory
	// that contains .cerberus/. The documented/default config location is
	// <root>/.cerberus/project.yaml, whose own directory is .cerberus, not the
	// root — so resolve one level up when the config sits directly inside a
	// .cerberus directory. A config at <root>/project.yaml keeps baseDir=root.
	configDir := filepath.Dir(path)
	baseDir := configDir
	if filepath.Base(configDir) == ".cerberus" {
		baseDir = filepath.Dir(configDir)
	}

	cfg, err := LoadFromYAML(data, baseDir)
	if err != nil {
		return nil, err
	}

	// Environment overlay: CERBERUS_ENV=staging → project.staging.yaml
	if env := os.Getenv("CERBERUS_ENV"); env != "" {
		envPath := filepath.Join(configDir, "project."+env+".yaml")
		if envData, err := os.ReadFile(envPath); err == nil {
			var overlay Config
			interpolated := envVarRE.ReplaceAllFunc(envData, func(match []byte) []byte {
				varName := string(match[2 : len(match)-1])
				if val := os.Getenv(varName); val != "" {
					return []byte(val)
				}
				return match
			})
			if err := yaml.Unmarshal(interpolated, &overlay); err != nil {
				return nil, fmt.Errorf("parse env overlay %s: %w", envPath, err)
			}
			if err := mergo.Merge(cfg, overlay, mergo.WithOverride); err != nil {
				return nil, fmt.Errorf("merge env overlay: %w", err)
			}
			// Re-apply defaults in case overlay zeroed fields that had defaults.
			applyDefaults(cfg)
			// Re-resolve protocol_ref in case the overlay introduced one; base
			// refs were already resolved (and cleared) by LoadFromYAML, so this
			// is a no-op for them.
			if err := resolveProtocolRefs(cfg, baseDir); err != nil {
				return nil, err
			}
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
		}
		// env file not found is not an error — env overlay is optional.
	}

	return cfg, nil
}

// resolveProtocolRefs loads each service's referenced protocol description
// file (.cerberus/protocols/<name>.yaml under baseDir) into svc.Protocol. It is
// called after applyDefaults and before Validate. Inline protocol and
// protocol_ref are mutually exclusive. baseDir == "" means files cannot be
// resolved (a protocol_ref then errors). On success the ref is cleared so the
// function is idempotent (the env-overlay re-validation path re-runs it).
func resolveProtocolRefs(cfg *Config, baseDir string) error {
	for i := range cfg.Services {
		svc := &cfg.Services[i]
		if svc.ProtocolRef == "" {
			continue
		}
		if svc.Protocol != nil {
			return fmt.Errorf("services[%d]: protocol and protocol_ref are mutually exclusive", i)
		}
		if baseDir == "" {
			return fmt.Errorf("services[%d]: protocol_ref %q requires loading from a project directory", i, svc.ProtocolRef)
		}
		if err := CheckProtocolRefName(svc.ProtocolRef); err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: %w", i, svc.ProtocolRef, err)
		}
		path := filepath.Join(baseDir, ".cerberus", "protocols", svc.ProtocolRef+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: %w", i, svc.ProtocolRef, err)
		}
		var p Protocol
		if err := yaml.Unmarshal(data, &p); err != nil {
			return fmt.Errorf("services[%d]: protocol_ref %q: parse: %w", i, svc.ProtocolRef, err)
		}
		svc.Protocol = &p
		svc.ProtocolRef = ""
	}
	return nil
}

// CheckProtocolRefName rejects a protocol_ref that could escape the protocols
// directory (path traversal). The ref must be a plain name. Exported so the
// `protocol infer` CLI can validate --name against the same rule before write.
func CheckProtocolRefName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("must be a plain name (no path separators or parent traversal)")
	}
	return nil
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

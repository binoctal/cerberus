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

	if err := mergeCredentials(cfg, configDir); err != nil {
		return nil, err
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
		// Load the routing vocabulary alongside the protocol when a vocab file
		// of the same name exists. Vocab is optional: a missing file is not an
		// error (the service simply has no Vocabulary for Scout prompt context).
		vocabPath := filepath.Join(baseDir, ".cerberus", "vocab", svc.ProtocolRef+".vocab.yaml")
		if vdata, verr := os.ReadFile(vocabPath); verr == nil {
			var v Vocabulary
			if perr := yaml.Unmarshal(vdata, &v); perr != nil {
				return fmt.Errorf("services[%d]: vocab %q: parse: %w", i, svc.ProtocolRef, perr)
			}
			svc.Vocabulary = &v
		}
		svc.ProtocolRef = ""
	}
	return nil
}

// credentialsFile is the on-disk shape of .cerberus/credentials.yaml: a map
// keyed by actor name. It is intentionally distinct from project.yaml's actor
// list form.
type credentialsFile struct {
	Actors map[string]credentialSecret `yaml:"actors"`
}

// credentialSecret mirrors the subset of CredentialRef that credentials.yaml
// may set: Email, Password, and the static WS Token.
type credentialSecret struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
	Token    string `yaml:"token"`
}

// mergeCredentials loads .cerberus/credentials.yaml from configDir (alongside
// the project config) and, for every actor present in both cfg and the file,
// overrides the actor's Email/Password/Token with the non-empty values from
// the file (layered override; env still wins via ResolveCredentials). A missing
// file is not an error — it is optional and gitignored. A present but malformed
// file is an error (fail loud), mirroring the env-overlay malformed handling.
func mergeCredentials(cfg *Config, configDir string) error {
	credPath := filepath.Join(configDir, "credentials.yaml")
	data, err := os.ReadFile(credPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // optional file
		}
		return fmt.Errorf("read credentials file: %w", err)
	}
	var cf credentialsFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return fmt.Errorf("parse credentials file %s: %w", credPath, err)
	}
	for i := range cfg.Actors {
		sec, ok := cf.Actors[cfg.Actors[i].Name]
		if !ok {
			continue
		}
		if sec.Email != "" {
			cfg.Actors[i].Credentials.Email = sec.Email
		}
		if sec.Password != "" {
			cfg.Actors[i].Credentials.Password = sec.Password
		}
		if sec.Token != "" {
			cfg.Actors[i].Credentials.Token = sec.Token
		}
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

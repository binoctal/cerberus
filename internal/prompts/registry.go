package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

//go:embed defaults/prompts.yaml
var defaultFS embed.FS

// Registry loads and serves prompt templates with project-level override support.
// Load order: embed.FS defaults → .cerberus/prompts/<key>.txt overrides.
type Registry struct {
	mu           sync.RWMutex
	prompts      map[string]string
	overridesDir string
	logger       *zap.Logger
}

// NewRegistry creates a prompt registry with defaults and optional overrides.
func NewRegistry(overridesDir string, logger *zap.Logger) *Registry {
	r := &Registry{
		prompts:      make(map[string]string),
		overridesDir: overridesDir,
		logger:       logger,
	}
	r.loadDefaults()
	if overridesDir != "" {
		r.loadOverrides()
	}
	return r
}

// Get returns a prompt template by key. Returns empty string if not found.
func (r *Registry) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.prompts[key]
}

// MustGet returns a prompt template by key, panics if not found.
func (r *Registry) MustGet(key string) string {
	p := r.Get(key)
	if p == "" {
		panic(fmt.Sprintf("prompt %q not found in registry", key))
	}
	return p
}

// Set sets a prompt template at runtime (useful for tests).
func (r *Registry) Set(key, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts[key] = value
}

// Keys returns all registered prompt keys.
func (r *Registry) Keys() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := make([]string, 0, len(r.prompts))
	for k := range r.prompts {
		keys = append(keys, k)
	}
	return keys
}

// loadDefaults loads prompts from the embedded prompts.yaml.
func (r *Registry) loadDefaults() {
	data, err := defaultFS.ReadFile("defaults/prompts.yaml")
	if err != nil {
		r.logger.Error("failed to read embedded prompts", zap.Error(err))
		return
	}

	// Simple YAML key: value parsing (no yaml dependency).
	lines := strings.Split(string(data), "\n")
	var currentKey string
	var currentValue strings.Builder
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")

		// Detect key line: "key:" at start (not indented).
		if !inBlock && !strings.HasPrefix(trimmed, " ") && !strings.HasPrefix(trimmed, "\t") && strings.Contains(trimmed, ": |") {
			// Save previous.
			if currentKey != "" {
				r.prompts[currentKey] = strings.TrimSpace(currentValue.String())
			}
			currentKey = strings.TrimSpace(strings.Split(trimmed, ":")[0])
			currentValue.Reset()
			inBlock = true
			continue
		}

		if inBlock {
			// Empty line or non-indented line ends the block.
			if trimmed == "" && currentValue.Len() > 0 {
				// Keep empty lines within the block.
				currentValue.WriteString("\n")
				continue
			}
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" {
				// New key detected — save previous.
				r.prompts[currentKey] = strings.TrimSpace(currentValue.String())
				currentKey = ""
				currentValue.Reset()
				inBlock = false

				// Re-process this line as potential new key.
				if strings.Contains(trimmed, ": |") {
					currentKey = strings.TrimSpace(strings.Split(trimmed, ":")[0])
					inBlock = true
				}
				continue
			}
			// Strip leading 2-space indent from YAML block scalar.
			content := strings.TrimPrefix(line, "  ")
			if currentValue.Len() > 0 {
				currentValue.WriteString("\n")
			}
			currentValue.WriteString(content)
		}
	}

	// Save last block.
	if currentKey != "" {
		r.prompts[currentKey] = strings.TrimSpace(currentValue.String())
	}

	r.logger.Info("loaded default prompts", zap.Int("count", len(r.prompts)))
}

// loadOverrides loads project-level prompt overrides from .cerberus/prompts/.
// Override files: <key>.txt — content replaces the default for that key.
func (r *Registry) loadOverrides() {
	entries, err := os.ReadDir(r.overridesDir)
	if err != nil {
		return // No overrides directory — fine.
	}

	loaded := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		key := strings.TrimSuffix(entry.Name(), ".txt")
		data, err := os.ReadFile(filepath.Join(r.overridesDir, entry.Name()))
		if err != nil {
			r.logger.Warn("failed to read prompt override", zap.String("file", entry.Name()), zap.Error(err))
			continue
		}

		r.prompts[key] = strings.TrimSpace(string(data))
		loaded++
	}

	if loaded > 0 {
		r.logger.Info("loaded prompt overrides", zap.Int("count", loaded))
	}
}

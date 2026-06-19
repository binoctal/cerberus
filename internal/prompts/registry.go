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

	lines := strings.Split(string(data), "\n")
	parser := &yamlBlockParser{
		prompts: r.prompts,
	}

	// Process all lines
	for _, line := range lines {
		parser.processLine(line)
	}

	// Save final block
	parser.saveCurrentBlock()

	r.logger.Info("loaded default prompts", zap.Int("count", len(r.prompts)))
}

// yamlBlockParser maintains state during YAML parsing
type yamlBlockParser struct {
	prompts      map[string]string
	currentKey   string
	currentValue strings.Builder
	inBlock      bool
}

// processLine processes a single line of YAML
func (p *yamlBlockParser) processLine(line string) {
	trimmed := strings.TrimRight(line, " \t")

	// Not in block - check for new key
	if !p.inBlock && isKeyLine(line, trimmed) {
		p.saveCurrentBlock()
		p.startNewBlock(trimmed)
		return
	}

	// In block - process content
	if p.inBlock {
		p.processInBlock(line, trimmed)
	}
}

// processInBlock handles lines when inside a YAML block
func (p *yamlBlockParser) processInBlock(line, trimmed string) {
	if p.handleEmptyInBlock(trimmed) {
		return
	}

	if p.handleBlockEnd(line, trimmed) {
		return
	}

	processBlockContent(line, &p.currentValue)
}

// handleEmptyInBlock handles empty lines within blocks
func (p *yamlBlockParser) handleEmptyInBlock(trimmed string) bool {
	if trimmed == "" && p.currentValue.Len() > 0 {
		p.currentValue.WriteString("\n")
		return true
	}
	return false
}

// handleBlockEnd handles block termination logic
func (p *yamlBlockParser) handleBlockEnd(line, trimmed string) bool {
	if shouldEndBlock(line, trimmed, p.currentValue.Len()) {
		p.saveCurrentBlock()
		p.resetBlock()

		// Re-process this line as potential new key
		if isKeyLine(line, trimmed) {
			p.startNewBlock(trimmed)
		}
		return true
	}
	return false
}

// startNewBlock starts a new YAML block with the given key line
func (p *yamlBlockParser) startNewBlock(trimmed string) {
	p.currentKey = extractKey(trimmed)
	p.currentValue.Reset()
	p.inBlock = true
}

// saveCurrentBlock saves the current block to prompts map
func (p *yamlBlockParser) saveCurrentBlock() {
	if p.currentKey != "" {
		p.prompts[p.currentKey] = strings.TrimSpace(p.currentValue.String())
	}
}

// resetBlock resets the parser state
func (p *yamlBlockParser) resetBlock() {
	p.currentKey = ""
	p.currentValue.Reset()
	p.inBlock = false
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

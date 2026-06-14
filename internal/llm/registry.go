package llm

import (
	_ "embed"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
)

//go:embed models.json
var embeddedModels []byte

// ModelCaps captures a model's token capacities.
type ModelCaps struct {
	Input  int `json:"input"`  // max prompt (input) tokens
	Output int `json:"output"` // max completion (output) tokens
}

// defaultContextWindow / defaultMaxOutput are the hard fallbacks used when the
// embedded JSON is unreadable or a field is zero. The normal unknown-model
// fallback comes from the JSON "defaults" block.
const (
	defaultContextWindow = 128_000
	defaultMaxOutput     = 4_096
)

// modelRegistry is the loaded model→caps table. prefixes holds the model keys
// sorted longest-first so longest-prefix matching wins (unexported, ignored by
// JSON).
type modelRegistry struct {
	Defaults ModelCaps            `json:"defaults"`
	Models   map[string]ModelCaps `json:"models"`
	prefixes []string
}

var (
	regMu sync.Mutex
	regV  *modelRegistry
)

// loadRegistry lazily loads the model table: the embedded models.json first,
// then an optional external JSON (CERBERUS_MODELS_JSON) merged on top so model
// caps can be overridden or extended at runtime without recompiling. The result
// is cached; call resetRegistry to force a reload (tests).
func loadRegistry() *modelRegistry {
	regMu.Lock()
	defer regMu.Unlock()
	if regV != nil {
		return regV
	}

	r := &modelRegistry{
		Defaults: ModelCaps{Input: defaultContextWindow, Output: defaultMaxOutput},
		Models:   map[string]ModelCaps{},
	}

	// Embedded default table (ships with the binary).
	if len(embeddedModels) > 0 {
		var tmp modelRegistry
		if json.Unmarshal(embeddedModels, &tmp) == nil {
			if tmp.Defaults.Input > 0 {
				r.Defaults.Input = tmp.Defaults.Input
			}
			if tmp.Defaults.Output > 0 {
				r.Defaults.Output = tmp.Defaults.Output
			}
			for k, v := range tmp.Models {
				r.Models[strings.ToLower(k)] = v
			}
		}
	}

	// External override: CERBERUS_MODELS_JSON points at a JSON file whose
	// "models" entries are merged on top of (and override) the embedded set.
	if path := os.Getenv("CERBERUS_MODELS_JSON"); path != "" {
		if data, err := os.ReadFile(path); err == nil {
			var ext modelRegistry
			if json.Unmarshal(data, &ext) == nil {
				if ext.Defaults.Input > 0 {
					r.Defaults.Input = ext.Defaults.Input
				}
				if ext.Defaults.Output > 0 {
					r.Defaults.Output = ext.Defaults.Output
				}
				for k, v := range ext.Models {
					r.Models[strings.ToLower(k)] = v
				}
			}
		}
	}

	r.prefixes = sortedPrefixes(r.Models)
	regV = r
	return r
}

// resetRegistry clears the cached registry so the next load re-reads the
// embedded table + CERBERUS_MODELS_JSON. Test-only.
func resetRegistry() {
	regMu.Lock()
	defer regMu.Unlock()
	regV = nil
}

func sortedPrefixes(m map[string]ModelCaps) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return keys
}

// resolveCaps finds capabilities for a model id: exact match, then longest
// prefix match (dated variants resolve to their family), then the table default.
func resolveCaps(model string) ModelCaps {
	// Model ids are matched case-insensitively: vendors and users mix case
	// (e.g. settings.json "GLM-4.5-Air" vs the lowercase registry key), and the
	// APIs treat them as equivalent.
	m := strings.ToLower(model)
	r := loadRegistry()
	if c, ok := r.Models[m]; ok {
		return c
	}
	for _, p := range r.prefixes {
		if strings.HasPrefix(m, p) {
			return r.Models[p]
		}
	}
	return r.Defaults
}

// ContextWindow returns the max input (prompt) token capacity for a model.
func ContextWindow(model string) int { return resolveCaps(model).Input }

// MaxOutput returns the max output (completion) token capacity for a model.
func MaxOutput(model string) int { return resolveCaps(model).Output }

package llm

import (
	"encoding/json"
	"os"
	"strings"
)

// newModelRegistry creates a new model registry with defaults
func newModelRegistry() *modelRegistry {
	return &modelRegistry{
		Defaults: ModelCaps{Input: defaultContextWindow, Output: defaultMaxOutput},
		Models:   map[string]ModelCaps{},
	}
}

// mergeRegistryData merges registry data from JSON into the registry
func mergeRegistryData(reg *modelRegistry, data []byte) {
	var tmp modelRegistry
	if json.Unmarshal(data, &tmp) != nil {
		return
	}

	// Merge defaults
	if tmp.Defaults.Input > 0 {
		reg.Defaults.Input = tmp.Defaults.Input
	}
	if tmp.Defaults.Output > 0 {
		reg.Defaults.Output = tmp.Defaults.Output
	}

	// Merge models (case-insensitive keys)
	for k, v := range tmp.Models {
		reg.Models[strings.ToLower(k)] = v
	}
}

// loadEmbeddedModels loads the embedded models.json into the registry
func loadEmbeddedModels(reg *modelRegistry) {
	if len(embeddedModels) == 0 {
		return
	}
	mergeRegistryData(reg, embeddedModels)
}

// loadExternalModels loads external override from CERBERUS_MODELS_JSON env var
func loadExternalModels(reg *modelRegistry) {
	path := os.Getenv("CERBERUS_MODELS_JSON")
	if path == "" {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	mergeRegistryData(reg, data)
}

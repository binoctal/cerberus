package business

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveBusinessModel saves business model to JSON file
func SaveBusinessModel(model *BusinessModel, path string) error {
	if model == nil {
		return fmt.Errorf("cannot save nil model")
	}

	// Validate before saving
	if err := model.Validate(); err != nil {
		return fmt.Errorf("model validation failed: %w", err)
	}

	data, err := json.MarshalIndent(model, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal model: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write model file: %w", err)
	}

	return nil
}

// LoadBusinessModel loads business model from JSON file
func LoadBusinessModel(path string) (*BusinessModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read model file: %w", err)
	}

	var model BusinessModel
	if err := json.Unmarshal(data, &model); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model: %w", err)
	}

	return &model, nil
}

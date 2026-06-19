package business

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemory_SaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	modelPath := filepath.Join(tmpDir, "business_model.json")

	model := &BusinessModel{
		ID:               "test-001",
		ProjectPath:      "/test/project",
		Domain:           "e-commerce",
		Confidence:       0.85,
		DomainConfidence: 0.9,
		Concepts: []BusinessConcept{
			{Name: "Order", Type: "entity", Confidence: 0.9},
		},
	}

	// Save model
	err := SaveBusinessModel(model, modelPath)
	if err != nil {
		t.Fatalf("Failed to save model: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		t.Error("Model file was not created")
	}

	// Load model
	loaded, err := LoadBusinessModel(modelPath)
	if err != nil {
		t.Fatalf("Failed to load model: %v", err)
	}

	// Verify content
	if loaded.ID != model.ID {
		t.Errorf("Expected ID %s, got %s", model.ID, loaded.ID)
	}

	if loaded.Confidence != model.Confidence {
		t.Errorf("Expected confidence %f, got %f", model.Confidence, loaded.Confidence)
	}
}

func TestMemory_SaveNilModel(t *testing.T) {
	err := SaveBusinessModel(nil, "/tmp/test.json")
	if err == nil {
		t.Error("Expected error for nil model, got nil")
	}
}

func TestMemory_LoadNonExistent(t *testing.T) {
	_, err := LoadBusinessModel("/tmp/nonexistent.json")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestMemory_LoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	modelPath := filepath.Join(tmpDir, "invalid.json")

	// Write invalid JSON
	_ = os.WriteFile(modelPath, []byte("{invalid json}"), 0644)

	_, err := LoadBusinessModel(modelPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

func TestMemory_SaveInvalidModel(t *testing.T) {
	tmpDir := t.TempDir()
	modelPath := filepath.Join(tmpDir, "invalid.json")

	invalidModel := &BusinessModel{
		ID:         "",  // Invalid: empty ID
		Confidence: 1.5, // Invalid: > 1.0
	}

	err := SaveBusinessModel(invalidModel, modelPath)
	if err == nil {
		t.Error("Expected error for invalid model, got nil")
	}
}

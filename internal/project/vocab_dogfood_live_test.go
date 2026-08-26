package project

import (
	"path/filepath"
	"testing"
)

// TestDogfoodVocabUILoads guards the dogfood vocab's ui section end-to-end:
// the on-disk yaml must decode and pass ValidateVocabulary (a broken
// denominator must not pass silently).
func TestDogfoodVocabUILoads(t *testing.T) {
	path := filepath.Join("..", "..", "dogfood", "realtime-e2e", ".cerberus", "vocab", "open-agents.vocab.yaml")
	v, err := LoadVocabulary(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := ValidateVocabulary(v); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if v.UI == nil {
		t.Fatal("dogfood vocab has no ui section")
	}
	if v.UI.BaseURL != "http://localhost:5183" || v.UI.Locale != "en" {
		t.Errorf("ui fields: %+v", v.UI)
	}
	nonExempt := 0
	for _, a := range v.UI.Assertions {
		if !a.Unsupported {
			nonExempt++
		}
	}
	if nonExempt == 0 {
		t.Error("no active ui assertions — the ui leg would add denominator only via nothing")
	}
}

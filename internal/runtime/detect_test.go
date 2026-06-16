package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsDevelopment(t *testing.T) {
	// Save current working directory
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	// Test 1: In cerberus project directory
	cerberusDir := t.TempDir()
	goModContent := `module github.com/binoctal/cerberus

go 1.25
`
	if err := os.WriteFile(filepath.Join(cerberusDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(cerberusDir); err != nil {
		t.Fatal(err)
	}

	if !IsDevelopment() {
		t.Error("Expected IsDevelopment() to return true in cerberus project")
	}

	// Test 2: In non-cerberus directory
	nonCerberusDir := t.TempDir()
	otherGoMod := `module github.com/example/other-project

go 1.25
`
	if err := os.WriteFile(filepath.Join(nonCerberusDir, "go.mod"), []byte(otherGoMod), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(nonCerberusDir); err != nil {
		t.Fatal(err)
	}

	if IsDevelopment() {
		t.Error("Expected IsDevelopment() to return false in non-cerberus project")
	}

	// Test 3: Directory without go.mod
	noGoModDir := t.TempDir()
	if err := os.Chdir(noGoModDir); err != nil {
		t.Fatal(err)
	}

	if IsDevelopment() {
		t.Error("Expected IsDevelopment() to return false when go.mod is missing")
	}
}

func TestGetPaths(t *testing.T) {
	// Save current working directory
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	// Test 1: In development environment
	devDir := t.TempDir()
	goModContent := `module github.com/binoctal/cerberus

go 1.25
`
	if err := os.WriteFile(filepath.Join(devDir, "go.mod"), []byte(goModContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(devDir); err != nil {
		t.Fatal(err)
	}

	paths := GetPaths()
	if paths.ConfigDir != filepath.Join(devDir, ".cerberus") {
		t.Errorf("Expected development paths, got ConfigDir: %s", paths.ConfigDir)
	}

	// Test 2: Not in development environment
	nonDevDir := t.TempDir()
	otherGoMod := `module github.com/example/other

go 1.25
`
	if err := os.WriteFile(filepath.Join(nonDevDir, "go.mod"), []byte(otherGoMod), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chdir(nonDevDir); err != nil {
		t.Fatal(err)
	}

	paths = GetPaths()
	// Should use system paths (not development)
	if paths.ConfigDir == filepath.Join(nonDevDir, ".cerberus") {
		t.Error("Expected system paths, not development paths")
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		{
			name:     "contains substring",
			s:        "hello world",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "does not contain substring",
			s:        "hello world",
			substr:   "goodbye",
			expected: false,
		},
		{
			name:     "empty substring",
			s:        "hello",
			substr:   "",
			expected: false,
		},
		{
			name:     "substring longer than string",
			s:        "hi",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "exact match",
			s:        "hello",
			substr:   "hello",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("contains(%q, %q) = %v, want %v",
					tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

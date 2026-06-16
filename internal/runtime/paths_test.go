package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	root := "/tmp/test-project"
	paths := New(root)

	expectedConfig := filepath.Join(root, ".cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedRuntime := filepath.Join(root, ".cerberus", "runtime")
	expectedData := filepath.Join(expectedRuntime, "data")
	if paths.DataDir != expectedData {
		t.Errorf("Expected DataDir %s, got %s", expectedData, paths.DataDir)
	}

	expectedLogs := filepath.Join(expectedRuntime, "logs")
	if paths.LogsDir != expectedLogs {
		t.Errorf("Expected LogsDir %s, got %s", expectedLogs, paths.LogsDir)
	}

	expectedCache := filepath.Join(expectedRuntime, "cache")
	if paths.CacheDir != expectedCache {
		t.Errorf("Expected CacheDir %s, got %s", expectedCache, paths.CacheDir)
	}

	expectedDB := filepath.Join(expectedData, "cerberus.db")
	if paths.DBPath != expectedDB {
		t.Errorf("Expected DBPath %s, got %s", expectedDB, paths.DBPath)
	}

	if paths.ProjectRoot != root {
		t.Errorf("Expected ProjectRoot %s, got %s", root, paths.ProjectRoot)
	}
}

func TestEnsure(t *testing.T) {
	tmpDir := t.TempDir()
	paths := New(tmpDir)

	err := paths.Ensure()
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	dirs := []string{
		paths.ConfigDir,
		paths.DataDir,
		paths.LogsDir,
		paths.CacheDir,
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("Directory %s should exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Errorf("%s should be a directory", dir)
		}
	}
}

func TestGetPaths(t *testing.T) {
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}

	paths := GetPaths()

	if paths.ProjectRoot != tmpDir {
		t.Errorf("Expected ProjectRoot %s, got %s", tmpDir, paths.ProjectRoot)
	}

	expectedConfig := filepath.Join(tmpDir, ".cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedDB := filepath.Join(tmpDir, ".cerberus", "runtime", "data", "cerberus.db")
	if paths.DBPath != expectedDB {
		t.Errorf("Expected DBPath %s, got %s", expectedDB, paths.DBPath)
	}
}

func TestIsDevelopment(t *testing.T) {
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)

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

	noGoModDir := t.TempDir()
	if err := os.Chdir(noGoModDir); err != nil {
		t.Fatal(err)
	}

	if IsDevelopment() {
		t.Error("Expected IsDevelopment() to return false when go.mod is missing")
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

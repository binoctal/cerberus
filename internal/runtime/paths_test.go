package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewDevelopmentPaths(t *testing.T) {
	root := "/tmp/cerberus-test"
	paths := newDevelopmentPaths(root)

	expectedConfig := filepath.Join(root, ".cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedRuntime := filepath.Join(root, "runtime")
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
}

func TestNewLinuxPaths(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux paths test on non-Linux system")
	}

	// Clear XDG env vars to test defaults
	os.Unsetenv("XDG_CONFIG_HOME")
	os.Unsetenv("XDG_DATA_HOME")
	os.Unsetenv("XDG_CACHE_HOME")

	paths := newLinuxPaths()

	home, _ := os.UserHomeDir()

	// Default: ~/.config/cerberus
	expectedConfig := filepath.Join(home, ".config", "cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	// Default: ~/.local/share/cerberus
	expectedData := filepath.Join(home, ".local", "share", "cerberus")
	if paths.DataDir != expectedData {
		t.Errorf("Expected DataDir %s, got %s", expectedData, paths.DataDir)
	}

	// Default: ~/.local/state/cerberus (logs)
	expectedLogs := filepath.Join(home, ".local", "state", "cerberus")
	if paths.LogsDir != expectedLogs {
		t.Errorf("Expected LogsDir %s, got %s", expectedLogs, paths.LogsDir)
	}

	// Default: ~/.cache/cerberus
	expectedCache := filepath.Join(home, ".cache", "cerberus")
	if paths.CacheDir != expectedCache {
		t.Errorf("Expected CacheDir %s, got %s", expectedCache, paths.CacheDir)
	}

	// DB path should be in data/data/
	expectedDB := filepath.Join(expectedData, "data", "cerberus.db")
	if paths.DBPath != expectedDB {
		t.Errorf("Expected DBPath %s, got %s", expectedDB, paths.DBPath)
	}
}

func TestNewLinuxPathsWithXDGEnv(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping Linux paths test on non-Linux system")
	}

	// Set custom XDG paths
	customConfig := "/tmp/test-config"
	customData := "/tmp/test-data"
	customCache := "/tmp/test-cache"

	os.Setenv("XDG_CONFIG_HOME", customConfig)
	os.Setenv("XDG_DATA_HOME", customData)
	os.Setenv("XDG_CACHE_HOME", customCache)
	defer func() {
		os.Unsetenv("XDG_CONFIG_HOME")
		os.Unsetenv("XDG_DATA_HOME")
		os.Unsetenv("XDG_CACHE_HOME")
	}()

	paths := newLinuxPaths()

	expectedConfig := filepath.Join(customConfig, "cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedData := filepath.Join(customData, "cerberus")
	if paths.DataDir != expectedData {
		t.Errorf("Expected DataDir %s, got %s", expectedData, paths.DataDir)
	}

	expectedCache := filepath.Join(customCache, "cerberus")
	if paths.CacheDir != expectedCache {
		t.Errorf("Expected CacheDir %s, got %s", expectedCache, paths.CacheDir)
	}

	// Logs still use ~/.local/state (not XDG override)
	home, _ := os.UserHomeDir()
	expectedLogs := filepath.Join(home, ".local", "state", "cerberus")
	if paths.LogsDir != expectedLogs {
		t.Errorf("Expected LogsDir %s, got %s", expectedLogs, paths.LogsDir)
	}
}

func TestNewMacOSPaths(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Skipping macOS paths test on non-macOS system")
	}

	paths := newMacOSPaths()
	home, _ := os.UserHomeDir()

	expectedConfig := filepath.Join(home, ".config", "cerberus")
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedData := filepath.Join(home, ".local", "share", "cerberus")
	if paths.DataDir != expectedData {
		t.Errorf("Expected DataDir %s, got %s", expectedData, paths.DataDir)
	}
}

func TestNewDockerPaths(t *testing.T) {
	paths := newDockerPaths()

	expectedConfig := "/app/config"
	if paths.ConfigDir != expectedConfig {
		t.Errorf("Expected ConfigDir %s, got %s", expectedConfig, paths.ConfigDir)
	}

	expectedData := "/app/data"
	if paths.DataDir != expectedData {
		t.Errorf("Expected DataDir %s, got %s", expectedData, paths.DataDir)
	}

	expectedLogs := "/app/logs"
	if paths.LogsDir != expectedLogs {
		t.Errorf("Expected LogsDir %s, got %s", expectedLogs, paths.LogsDir)
	}

	expectedCache := "/app/cache"
	if paths.CacheDir != expectedCache {
		t.Errorf("Expected CacheDir %s, got %s", expectedCache, paths.CacheDir)
	}

	expectedDB := "/app/data/cerberus.db"
	if paths.DBPath != expectedDB {
		t.Errorf("Expected DBPath %s, got %s", expectedDB, paths.DBPath)
	}
}

func TestEnsure(t *testing.T) {
	tmpDir := t.TempDir()
	paths := newDevelopmentPaths(tmpDir)

	// Ensure all directories
	err := paths.Ensure()
	if err != nil {
		t.Fatalf("Ensure failed: %v", err)
	}

	// Check that directories exist
	dirs := []string{
		paths.ConfigDir,
		filepath.Join(paths.DataDir, "data"),
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

func TestGetEnv(t *testing.T) {
	// Test with env var set
	os.Setenv("TEST_VAR", "test-value")
	if got := getEnv("TEST_VAR", "fallback"); got != "test-value" {
		t.Errorf("Expected 'test-value', got '%s'", got)
	}
	os.Unsetenv("TEST_VAR")

	// Test with fallback
	if got := getEnv("NON_EXISTENT_VAR", "fallback"); got != "fallback" {
		t.Errorf("Expected 'fallback', got '%s'", got)
	}
}

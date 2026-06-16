package runtime

import (
	"os"
	"path/filepath"
	"runtime"
)

// Paths holds all runtime file paths for Cerberus
type Paths struct {
	// ConfigDir configuration files directory
	ConfigDir string
	// DataDir persistent data directory (database, etc.)
	DataDir string
	// LogsDir log files directory
	LogsDir string
	// CacheDir temporary cache directory
	CacheDir string
	// DBPath SQLite database file path
	DBPath string
}

// New creates runtime paths based on the current operating system and environment
func New() *Paths {
	// Check if running in Docker first
	if isInDocker() {
		return newDockerPaths()
	}

	// Select paths based on OS
	switch runtime.GOOS {
	case "windows":
		return newWindowsPaths()
	case "darwin":
		return newMacOSPaths()
	default: // linux, *bsd
		return newLinuxPaths()
	}
}

// newDevelopmentPaths creates paths for development environment
func newDevelopmentPaths(projectRoot string) *Paths {
	runtimeDir := filepath.Join(projectRoot, "runtime")
	return &Paths{
		ConfigDir: filepath.Join(projectRoot, ".cerberus"),
		DataDir:   filepath.Join(runtimeDir, "data"),
		LogsDir:   filepath.Join(runtimeDir, "logs"),
		CacheDir:  filepath.Join(runtimeDir, "cache"),
		DBPath:    filepath.Join(runtimeDir, "data", "cerberus.db"),
	}
}

// newLinuxPaths creates paths following XDG Base Directory Specification
func newLinuxPaths() *Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}

	// XDG environment variables with fallbacks
	configHome := getEnv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	dataHome := getEnv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	cacheHome := getEnv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	stateHome := filepath.Join(home, ".local", "state") // logs

	return &Paths{
		ConfigDir: filepath.Join(configHome, "cerberus"),
		DataDir:   filepath.Join(dataHome, "cerberus"),
		LogsDir:   filepath.Join(stateHome, "cerberus"),
		CacheDir:  filepath.Join(cacheHome, "cerberus"),
		DBPath:    filepath.Join(dataHome, "cerberus", "data", "cerberus.db"),
	}
}

// newMacOSPaths creates paths for macOS (similar to Linux)
func newMacOSPaths() *Paths {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}

	return &Paths{
		ConfigDir: filepath.Join(home, ".config", "cerberus"),
		DataDir:   filepath.Join(home, ".local", "share", "cerberus"),
		LogsDir:   filepath.Join(home, ".local", "state", "cerberus"),
		CacheDir:  filepath.Join(home, ".cache", "cerberus"),
		DBPath:    filepath.Join(home, ".local", "share", "cerberus", "data", "cerberus.db"),
	}
}

// newWindowsPaths creates paths for Windows
func newWindowsPaths() *Paths {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}

	return &Paths{
		ConfigDir: filepath.Join(appData, "Cerberus"),
		DataDir:   filepath.Join(localAppData, "Cerberus"),
		LogsDir:   filepath.Join(localAppData, "Cerberus", "logs"),
		CacheDir:  filepath.Join(localAppData, "Cerberus", "cache"),
		DBPath:    filepath.Join(localAppData, "Cerberus", "data", "cerberus.db"),
	}
}

// newDockerPaths creates paths for Docker container
func newDockerPaths() *Paths {
	return &Paths{
		ConfigDir: "/app/config",
		DataDir:   "/app/data",
		LogsDir:   "/app/logs",
		CacheDir:  "/app/cache",
		DBPath:    "/app/data/cerberus.db",
	}
}

// isInDocker detects if running inside a Docker container
func isInDocker() bool {
	// Method 1: check for /.dockerenv file
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	// Method 2: check /proc/1/cgroup (could add more detection methods)
	return false
}

// Ensure creates all runtime directories if they don't exist
func (p *Paths) Ensure() error {
	dirs := []string{
		p.ConfigDir,
		p.DataDir, // Database will be in DataDir directly
		p.LogsDir,
		p.CacheDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// getEnv returns the environment variable value or fallback if not set
func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

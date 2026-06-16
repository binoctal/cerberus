package runtime

import (
	"os"
	"path/filepath"
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
	// ProjectRoot project directory
	ProjectRoot string
}

// New creates runtime paths based on the current project directory
// All runtime files are stored in the project directory under .cerberus/
func New(projectRoot string) *Paths {
	runtimeDir := filepath.Join(projectRoot, ".cerberus", "runtime")
	return &Paths{
		ProjectRoot: projectRoot,
		ConfigDir:   filepath.Join(projectRoot, ".cerberus"),
		DataDir:     filepath.Join(runtimeDir, "data"),
		LogsDir:     filepath.Join(runtimeDir, "logs"),
		CacheDir:    filepath.Join(runtimeDir, "cache"),
		DBPath:      filepath.Join(runtimeDir, "data", "cerberus.db"),
	}
}

// Ensure creates all runtime directories if they don't exist
func (p *Paths) Ensure() error {
	dirs := []string{
		p.ConfigDir,
		p.DataDir,
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

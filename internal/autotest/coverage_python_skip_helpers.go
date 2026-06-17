package autotest

import (
	"path/filepath"
	"strings"
)

// isPythonCacheFile checks if a file is a Python cache file
func isPythonCacheFile(base string) bool {
	return strings.HasSuffix(base, ".pyc") ||
		base == "__init__.py"
}

// containsPythonCachePath checks if path contains __pycache__
func containsPythonCachePath(path string) bool {
	return strings.Contains(path, "__pycache__")
}

// isPythonCacheArtifact checks if the file is any Python cache artifact
func isPythonCacheArtifact(path string) bool {
	base := filepath.Base(path)
	return isPythonCacheFile(base) || containsPythonCachePath(path)
}

// excludedDirectories returns list of directories to exclude from coverage
func excludedDirectories() []string {
	return []string{".git", "venv", ".venv", "env", "dist", "build", ".pytest_cache"}
}

// isInExcludedDir checks if path is in an excluded directory
func isInExcludedDir(path string) bool {
	excluded := excludedDirectories()
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		for _, excl := range excluded {
			if seg == excl {
				return true
			}
		}
	}
	return false
}

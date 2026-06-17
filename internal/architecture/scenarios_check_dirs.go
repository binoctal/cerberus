package architecture

import (
	"os"
	"path/filepath"
)

// checkDocumentationDirectories checks if documentation directories exist
func (a *Analyzer) checkDocumentationDirectories() bool {
	docDirs := []string{
		"cerberus-docs",
		"docs",
		"design",
		"documentation",
		".cerberus",
	}

	for _, dir := range docDirs {
		if _, err := os.Stat(filepath.Join(a.projectPath, dir)); err == nil {
			return true
		}
	}

	return false
}

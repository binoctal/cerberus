package autotest

import (
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

// shouldSkipNodeFile determines if a Node.js file should be excluded from coverage
func shouldSkipNodeFile(path string) bool {
	base := filepath.Base(path)

	// Skip generated files
	if strings.Contains(base, ".min.js") ||
		strings.Contains(base, ".bundle.js") ||
		strings.Contains(base, "-bundle.js") {
		return true
	}

	// Skip node_modules and other common exclusions
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == "node_modules" ||
			seg == ".git" ||
			seg == "dist" ||
			seg == "build" ||
			seg == "coverage" {
			return true
		}
	}

	return false
}

// SetLogger sets the logger for the provider
func (p *NodeCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}

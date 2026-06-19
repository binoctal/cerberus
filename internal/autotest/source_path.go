package autotest

import (
	"os"
	"path/filepath"
	"strings"
)

// sourcePath resolves a coverage gap's file path (go cover emits module-
// qualified paths like github.com/x/y/internal/...) to a filesystem path
// under projectDir. The module prefix (from go.mod) is stripped; if it cannot
// be determined the path is treated as already filesystem-relative.
func sourcePath(gapFile, projectDir string) string {
	rel := gapFile
	if mod := moduleName(projectDir); mod != "" {
		rel = strings.TrimPrefix(gapFile, mod+"/")
	}
	return filepath.Join(projectDir, rel)
}

// moduleName reads the module path from projectDir/go.mod. Empty on any error
// (the caller then treats gap paths as filesystem-relative).
func moduleName(projectDir string) string {
	b, err := os.ReadFile(filepath.Join(projectDir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	return ""
}

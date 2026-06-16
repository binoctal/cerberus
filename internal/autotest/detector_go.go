package autotest

import (
	"os"
	"os/exec"
	"path/filepath"
)

// GoProjectDetector detects Go projects
type GoProjectDetector struct{}

func (d *GoProjectDetector) Type() ProjectType { return ProjectTypeGo }

func (d *GoProjectDetector) Detect(projectDir string) (bool, float64, string) {
	// Check for go.mod
	goModPath := filepath.Join(projectDir, "go.mod")
	if _, err := os.Stat(goModPath); os.IsNotExist(err) {
		return false, 0, ""
	}

	// Verify go command is available
	_, err := exec.LookPath("go")
	if err != nil {
		return false, 0.5, "" // go.mod exists but Go not installed
	}

	return true, 1.0, "go"
}

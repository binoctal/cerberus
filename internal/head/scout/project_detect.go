package scout

import "os"

// ProjectType identifies the kind of project being tested.
type ProjectType string

const (
	ProjectUnknown ProjectType = "unknown"
	ProjectGo      ProjectType = "go"
	ProjectNode    ProjectType = "node"
	ProjectPython  ProjectType = "python"
	ProjectHTTP    ProjectType = "http"
)

// ProjectInfo holds detected project metadata used to generate executor cases.
type ProjectInfo struct {
	Type       ProjectType
	RootDir    string
	BuildCmd   string
	TestCmd    string
	LintCmd    string
	Entrypoint string
	Language   string
}

// DetectProjectType identifies the project type by checking for well-known file
// signatures in the given root directory. Returns ProjectHTTP when rootDir is empty.
func DetectProjectType(rootDir string) ProjectInfo {
	if rootDir == "" {
		return ProjectInfo{Type: ProjectHTTP}
	}

	// Check for Go project.
	if _, err := os.Stat(rootDir + "/go.mod"); err == nil {
		return ProjectInfo{
			Type:     ProjectGo,
			RootDir:  rootDir,
			BuildCmd: "go build ./...",
			TestCmd:  "go test ./...",
			LintCmd:  "go vet ./...",
			Language: "Go",
		}
	}

	// Check for Node.js project.
	if _, err := os.Stat(rootDir + "/package.json"); err == nil {
		return ProjectInfo{
			Type:     ProjectNode,
			RootDir:  rootDir,
			BuildCmd: "npm install",
			TestCmd:  "npm test",
			Language: "JavaScript/TypeScript",
		}
	}

	// Check for Python project.
	pythonMarkers := []string{"pyproject.toml", "setup.py", "requirements.txt"}
	for _, marker := range pythonMarkers {
		if _, err := os.Stat(rootDir + "/" + marker); err == nil {
			return ProjectInfo{
				Type:     ProjectPython,
				RootDir:  rootDir,
				TestCmd:  "pytest",
				LintCmd:  "ruff check",
				Language: "Python",
			}
		}
	}

	return ProjectInfo{Type: ProjectUnknown, RootDir: rootDir}
}

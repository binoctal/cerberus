package autotest

// ProjectDetector detects if a project supports a specific test framework
type ProjectDetector interface {
	Detect(projectDir string) (supported bool, confidence float64, toolPath string)
	Type() ProjectType
}

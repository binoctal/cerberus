package architecture

// NewAnalyzer creates a new architecture analyzer
func NewAnalyzer(projectPath string) *Analyzer {
	return &Analyzer{
		projectPath: projectPath,
	}
}

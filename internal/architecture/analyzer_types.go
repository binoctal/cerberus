package architecture

// Analyzer performs architecture analysis
type Analyzer struct {
	projectPath string
	maxLinesFile string
}

// FileComplexityMetrics represents metrics for a single file
type FileComplexityMetrics struct {
	Lines      int
	Functions  int
	MaxParams  int
	MaxDepth   int
}

package architecture

import (
	"os"
	"strings"
)

// analyzeFileComplexity analyzes complexity of a single file
func (a *Analyzer) analyzeFileComplexity(filePath string, report *ArchitectureReport) ([]ArchitectureIssue, *FileComplexityMetrics, error) {
	issues := []ArchitectureIssue{}
	metrics := &FileComplexityMetrics{}

	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, err
	}

	lines := strings.Split(string(content), "\n")

	// Phase 1: Count code lines
	codeLines := countCodeLines(lines)
	metrics.Lines = codeLines

	// Phase 2: Check if file too long
	if codeLines > 150 {
		issue := reportLongFile(filePath, a.projectPath, codeLines, report)
		issues = append(issues, issue)
	}

	// Phase 3: Count functions
	metrics.Functions = countFunctions(lines)

	return issues, metrics, nil
}

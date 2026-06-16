package architecture

import (
	"fmt"
	"os"
	"path/filepath"
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
	metrics.Lines = len(lines)

	// Count code lines (excluding comments and blank lines)
	codeLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			codeLines++
		}
	}

	// Check: File too long
	if codeLines > 150 {
		relPath, _ := filepath.Rel(a.projectPath, filePath)
		issues = append(issues, ArchitectureIssue{
			ID:          fmt.Sprintf("file-too-long-%s", strings.ReplaceAll(relPath, "/", "-")),
			Type:        OverEngineering,
			Severity:    SeverityWarning,
			File:        relPath,
			Line:        0,
			Description: fmt.Sprintf("文件有 %d 行代码，超过阈值 150 行", codeLines),
			Rationale:   "长文件通常包含过多职责，难以维护和测试",
			Suggestion:  "考虑拆分为多个文件，每个文件单一职责",
			Confidence:  1.0,
			Evidence:    []string{fmt.Sprintf("实际行数: %d", codeLines)},
		})

		// Track max
		if codeLines > report.Metrics.MaxLinesPerFile {
			report.Metrics.MaxLinesPerFile = codeLines
			a.maxLinesFile = relPath
		}
	}

	// Count functions
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			metrics.Functions++
		}
	}

	metrics.Lines = codeLines

	return issues, metrics, nil
}

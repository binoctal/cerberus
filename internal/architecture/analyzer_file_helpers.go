package architecture

import (
	"fmt"
	"path/filepath"
	"strings"
)

// countCodeLines counts non-comment, non-blank lines
func countCodeLines(lines []string) int {
	codeLines := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "/*") {
			codeLines++
		}
	}
	return codeLines
}

// countFunctions counts function declarations
func countFunctions(lines []string) int {
	funcCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			funcCount++
		}
	}
	return funcCount
}

// reportLongFile reports when a file exceeds the line threshold
func reportLongFile(filePath, projectPath string, codeLines int, report *ArchitectureReport) ArchitectureIssue {
	relPath, _ := filepath.Rel(projectPath, filePath)

	issue := ArchitectureIssue{
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
	}

	// Track max
	if codeLines > report.Metrics.MaxLinesPerFile {
		report.Metrics.MaxLinesPerFile = codeLines
	}

	return issue
}

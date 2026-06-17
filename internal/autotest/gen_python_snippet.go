package autotest

import (
	"fmt"
	"path/filepath"
	"strings"
)

// snippetBuilder constructs code snippets from AST info
type snippetBuilder struct {
	astInfo []PythonAstInfo
}

// newSnippetBuilder creates a new snippet builder
func newSnippetBuilder(astInfo []PythonAstInfo) *snippetBuilder {
	return &snippetBuilder{astInfo: astInfo}
}

// build constructs the final snippet string
func (sb *snippetBuilder) build() string {
	var snippets []string
	for _, info := range sb.astInfo {
		snippets = append(snippets, sb.formatInfo(info))
	}
	return strings.Join(snippets, "\n")
}

// formatInfo formats a single AST info as a snippet line
func (sb *snippetBuilder) formatInfo(info PythonAstInfo) string {
	switch info.Type {
	case "class":
		return fmt.Sprintf("class %s:  # line %d", info.Name, info.LineNo)
	case "function":
		return fmt.Sprintf("def %s(...):  # line %d", info.Name, info.LineNo)
	case "method":
		return fmt.Sprintf("    def %s(...):  # line %d (in %s)", info.Name, info.LineNo, info.ClassName)
	default:
		return ""
	}
}

// buildModuleSnippet creates a module name and snippet from AST info
func buildModuleSnippet(funcName string, astInfo []PythonAstInfo) (string, string) {
	moduleName := extractModuleName(funcName)
	snippet := newSnippetBuilder(astInfo).build()
	return moduleName, snippet
}

// extractModuleName extracts the module name from a file path
func extractModuleName(funcName string) string {
	moduleName := filepath.Base(funcName)
	moduleName = strings.TrimSuffix(moduleName, ".py")
	return moduleName
}

// extractLineBasedSnippet extracts a snippet around a specific line number
func extractLineBasedSnippet(source string, lineNum int, contextLines int) string {
	lines := strings.Split(source, "\n")
	if lineNum <= 0 || lineNum > len(lines) {
		return ""
	}

	start := lineNum - contextLines
	if start < 0 {
		start = 0
	}
	end := lineNum + contextLines
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start:end], "\n")
}

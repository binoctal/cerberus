package autotest

import (
	"fmt"
	"strings"
)

// funcNameParser handles parsing of function names with line numbers
type funcNameParser struct {
	funcName string
	lineNum  int
}

// parseFuncName extracts line number from function name format "name:L123"
func parseFuncName(funcName string) funcNameParser {
	parser := funcNameParser{funcName: funcName, lineNum: 0}

	if strings.Contains(funcName, ":L") {
		parts := strings.Split(funcName, ":L")
		if len(parts) == 2 {
			if n, err := fmt.Sscanf(parts[1], "%d", &parser.lineNum); err == nil && n > 0 {
				parser.funcName = parts[0]
			}
		}
	}

	return parser
}

// extractionStrategy defines the strategy for extracting code
type extractionStrategy interface {
	extract() (string, string)
}

// astExtraction uses Python AST to extract structured info
type astExtraction struct {
	pythonCmd string
	source    string
	parser    funcNameParser
}

func (e *astExtraction) extract() (string, string) {
	astInfo, err := extractPythonAST(e.pythonCmd, e.source, e.parser.lineNum)
	if err != nil || len(astInfo) == 0 {
		return "", ""
	}

	return buildModuleSnippet(e.parser.funcName, astInfo)
}

// lineBasedExtraction uses line number context
type lineBasedExtraction struct {
	source   string
	parser   funcNameParser
	context  int
}

func (e *lineBasedExtraction) extract() (string, string) {
	snippet := extractLineBasedSnippet(e.source, e.parser.lineNum, e.context)
	if snippet == "" {
		return "", ""
	}

	moduleName := extractModuleName(e.parser.funcName)
	return moduleName, snippet
}

// fullSourceExtraction returns entire source as fallback
type fullSourceExtraction struct {
	parser funcNameParser
	source string
}

func (e *fullSourceExtraction) extract() (string, string) {
	moduleName := extractModuleName(e.parser.funcName)
	return moduleName, e.source
}

// extractPythonFunction extracts a function/class from source using Python ast module
func extractPythonFunction(pythonCmd string, source []byte, funcName string) (string, string) {
	src := string(source)
	parser := parseFuncName(funcName)

	// Strategy 1: Try AST-based extraction
	if pythonCmd != "" {
		strategy := &astExtraction{pythonCmd: pythonCmd, source: src, parser: parser}
		if moduleName, snippet := strategy.extract(); snippet != "" {
			return moduleName, snippet
		}
	}

	// Strategy 2: Line-based context extraction
	if parser.lineNum > 0 {
		strategy := &lineBasedExtraction{source: src, parser: parser, context: 20}
		if moduleName, snippet := strategy.extract(); snippet != "" {
			return moduleName, snippet
		}
	}

	// Strategy 3: Full source fallback
	strategy := &fullSourceExtraction{parser: parser, source: src}
	return strategy.extract()
}

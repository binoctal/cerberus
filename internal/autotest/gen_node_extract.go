package autotest

import (
	"path/filepath"
	"strings"
)

var nodePatterns = []struct {
	pattern string
	name    string
}{
	{`export\s+(?:async\s+)?function\s+(\w+)`, "function"},
	{`export\s+class\s+(\w+)`, "class"},
	{`export\s+(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>)`, "arrow-function"},
	{`export\s+default\s+(?:async\s+)?function\s+(\w+)`, "default-function"},
	{`export\s+default\s+class\s+(\w+)`, "default-class"},
	{`export\s+default\s+(?:async\s+)?(?:function|\([^)]*\)\s*=>)`, "default-arrow"},
}

func extractNodeFunction(source []byte, funcName string) (string, string) {
	src := string(source)

	lineNum := 0
	if strings.Contains(funcName, ":L") {
		parts := strings.Split(funcName, ":L")
		if len(parts) == 2 {
			lineNum = parseIntOrZero(parts[1])
			funcName = parts[0]
		}
	}

	pkg := funcName
	if strings.Contains(funcName, ".js") {
		pkg = filepath.Base(funcName)
	}

	if lineNum > 0 {
		lines := strings.Split(src, "\n")
		if lineNum <= len(lines) {
			start := lineNum - 20
			if start < 0 {
				start = 0
			}
			end := lineNum + 20
			if end > len(lines) {
				end = len(lines)
			}
			snippet := strings.Join(lines[start:end], "\n")
			return pkg, snippet
		}
	}

	return pkg, src
}

func matchNodeFunctions(source string) []FunctionInfo {
	var functions []FunctionInfo

	lines := strings.Split(source, "\n")
	for _, line := range lines {
		for _, pat := range nodePatterns {
			if strings.Contains(line, "function") || strings.Contains(line, "class") {
				functions = append(functions, FunctionInfo{
					Name: "extracted",
					Type: pat.name,
				})
			}
		}
	}

	return functions
}

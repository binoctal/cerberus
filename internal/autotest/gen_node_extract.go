package autotest

import (
	"path/filepath"
	"strings"
)

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

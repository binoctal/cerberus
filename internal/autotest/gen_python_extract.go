package autotest

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// extractPythonFunction extracts a function/class from source using Python ast module
func extractPythonFunction(pythonCmd string, source []byte, funcName string) (string, string) {
	src := string(source)

	// Extract line number if present
	lineNum := 0
	if strings.Contains(funcName, ":L") {
		parts := strings.Split(funcName, ":L")
		if len(parts) == 2 {
			if n, err := fmt.Sscanf(parts[1], "%d", &lineNum); err == nil && n > 0 {
				// Successfully parsed line number
			}
			funcName = parts[0]
		}
	}

	// Try to extract using Python ast module
	if pythonCmd != "" {
		astInfo, err := extractPythonAST(pythonCmd, src, lineNum)
		if err == nil && len(astInfo) > 0 {
			// Found AST info, construct snippet
			moduleName := filepath.Base(funcName)
			moduleName = strings.TrimSuffix(moduleName, ".py")

			// Build snippet with function signatures
			var snippets []string
			for _, info := range astInfo {
				if info.Type == "class" {
					snippets = append(snippets, fmt.Sprintf("class %s:  # line %d", info.Name, info.LineNo))
				} else if info.Type == "function" {
					snippets = append(snippets, fmt.Sprintf("def %s(...):  # line %d", info.Name, info.LineNo))
				} else if info.Type == "method" {
					snippets = append(snippets, fmt.Sprintf("    def %s(...):  # line %d (in %s)", info.Name, info.LineNo, info.ClassName))
				}
			}

			if len(snippets) > 0 {
				return moduleName, strings.Join(snippets, "\n")
			}
		}
	}

	// Fallback: extract around line number
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
			moduleName := filepath.Base(funcName)
			moduleName = strings.TrimSuffix(moduleName, ".py")
			return moduleName, snippet
		}
	}

	// Final fallback: return the entire source
	moduleName := filepath.Base(funcName)
	moduleName = strings.TrimSuffix(moduleName, ".py")
	return moduleName, src
}

// extractPythonAST uses Python's ast module to extract function/class info
func extractPythonAST(pythonCmd, source string, targetLine int) ([]PythonAstInfo, error) {
	// Create a temporary Python script to extract AST info
	script := `
import ast
import json
import sys

source = sys.stdin.read()
try:
    tree = ast.parse(source)
except SyntaxError:
    print(json.dumps([]))
    sys.exit(0)

functions = []

for node in ast.walk(tree):
    if isinstance(node, ast.FunctionDef):
        info = {
            "name": node.name,
            "type": "function",
            "lineno": node.lineno,
            "class": "",
            "docstring": ast.get_docstring(node),
            "args": [arg.arg for arg in node.args.args]
        }
        functions.append(info)
    elif isinstance(node, ast.ClassDef):
        info = {
            "name": node.name,
            "type": "class",
            "lineno": node.lineno,
            "class": "",
            "docstring": ast.get_docstring(node),
            "args": []
        }
        functions.append(info)
        # Extract methods
        for item in node.body:
            if isinstance(item, ast.FunctionDef):
                method_info = {
                    "name": item.name,
                    "type": "method",
                    "lineno": item.lineno,
                    "class": node.name,
                    "docstring": ast.get_docstring(item),
                    "args": [arg.arg for arg in item.args.args]
                }
                functions.append(method_info)

print(json.dumps(functions))
`

	cmd := exec.Command(pythonCmd, "-c", script)
	cmd.Stdin = strings.NewReader(source)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("python ast extraction failed: %w", err)
	}

	var infos []PythonAstInfo
	if err := json.Unmarshal(output, &infos); err != nil {
		return nil, fmt.Errorf("python ast parse failed: %w", err)
	}

	// Filter to functions near target line if specified
	if targetLine > 0 {
		var filtered []PythonAstInfo
		for _, info := range infos {
			// Include functions within 20 lines of target
			if abs(info.LineNo-targetLine) <= 20 {
				filtered = append(filtered, info)
			}
		}
		// If no nearby functions, include all
		if len(filtered) > 0 {
			infos = filtered
		}
	}

	return infos, nil
}

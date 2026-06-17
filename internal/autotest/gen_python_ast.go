package autotest

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// pythonASTScript is the Python script for AST extraction
const pythonASTScript = `
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

// extractPythonAST uses Python's ast module to extract function/class info
func extractPythonAST(pythonCmd, source string, targetLine int) ([]PythonAstInfo, error) {
	cmd := exec.Command(pythonCmd, "-c", pythonASTScript)
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
		infos = filterByProximity(infos, targetLine, 20)
	}

	return infos, nil
}

// filterByProximity filters AST info by proximity to target line
func filterByProximity(infos []PythonAstInfo, targetLine, maxDistance int) []PythonAstInfo {
	var filtered []PythonAstInfo
	for _, info := range infos {
		if abs(info.LineNo-targetLine) <= maxDistance {
			filtered = append(filtered, info)
		}
	}
	// If no nearby functions, include all
	if len(filtered) > 0 {
		return filtered
	}
	return infos
}

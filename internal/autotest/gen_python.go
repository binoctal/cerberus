package autotest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

// PythonTestGenerator generates pytest tests for Python projects
type PythonTestGenerator struct {
	driver     *ai.Driver
	logger     *zap.Logger
	pythonCmd  string
}

// NewPythonTestGenerator creates a new Python test generator
func NewPythonTestGenerator(driver interface{}) *PythonTestGenerator {
	var d *ai.Driver
	if v, ok := driver.(*ai.Driver); ok {
		d = v
	}
	return &PythonTestGenerator{
		driver:    d,
		logger:    zap.NewNop(),
		pythonCmd: "python3",
	}
}

// PythonAstInfo represents extracted function/class info from Python AST
type PythonAstInfo struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"` // "function", "class", "method"
	LineNo    int      `json:"lineno"`
	ClassName string   `json:"class,omitempty"`
	Docstring string   `json:"docstring,omitempty"`
	Args      []string `json:"args,omitempty"`
}

// Generate generates a pytest test file for a coverage gap
func (g *PythonTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	// Extract function/class info using Python ast
	pkg, snippet := extractPythonFunction(g.pythonCmd, source, gap.Func)

	prompt := g.buildPrompt(pkg, gap.File, snippet)

	// Try JSON response first
	var out struct {
		Test string `json:"test"`
	}

	err := g.driver.Decide(ctx, prompt, &out)
	if err == nil && strings.TrimSpace(out.Test) != "" {
		content := []byte(stripFences(out.Test))
		return TestFile{
			Path:    pythonTestFilePath(gap.File),
			Content: content,
		}, nil
	}

	// Fallback: use raw completion
	resp, rerr := g.driver.Client().Complete(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	})
	if rerr != nil {
		return TestFile{}, fmt.Errorf("python gen: decide %w, fallback %w", err, rerr)
	}

	content := []byte(stripFences(resp.Content))
	return TestFile{
		Path:    pythonTestFilePath(gap.File),
		Content: content,
	}, nil
}

// buildPrompt creates the prompt for test generation
func (g *PythonTestGenerator) buildPrompt(pkg, file, snippet string) string {
	return fmt.Sprintf(`You are a pytest test author. Emit a single complete test_*.py file using pytest fixtures and parametrize.
Use modern Python (3.7+) and pytest best practices.
Include proper imports, fixtures, and parameterized test cases using @pytest.mark.parametrize.
Output ONLY the Python source, no markdown fences.

Write a pytest test file for this code.

File: %s
Module: %s

Source code:
%s

Return a complete test_*.py file with:
- Proper imports (including pytest, unittest.mock if needed)
- Fixture functions using @pytest.fixture
- Parameterized test cases using @pytest.mark.parametrize
- Test class structure if testing a class
- Edge cases and error handling
- Clear test names that describe what is being tested`, file, pkg, snippet)
}

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

// pythonTestFilePath returns the test file path for a Python source file
func pythonTestFilePath(sourceFile string) string {
	// Check if there's a tests/ directory at project root
	projectRoot := findProjectRoot(sourceFile)
	testsDir := filepath.Join(projectRoot, "tests")

	if _, err := os.Stat(testsDir); err == nil {
		// Use tests/ directory
		relPath, _ := filepath.Rel(projectRoot, sourceFile)
		testRelPath := "test_" + filepath.Base(relPath)
		return filepath.Join(testsDir, testRelPath)
	}

	// Default: same directory as source
	dir := filepath.Dir(sourceFile)
	base := filepath.Base(sourceFile)
	name := strings.TrimSuffix(base, ".py")
	return filepath.Join(dir, "test_"+name+".py")
}

// SetLogger sets the logger for the generator
func (g *PythonTestGenerator) SetLogger(logger *zap.Logger) {
	g.logger = logger
}

// SetPythonCmd sets the Python command to use for AST extraction
func (g *PythonTestGenerator) SetPythonCmd(cmd string) {
	g.pythonCmd = cmd
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

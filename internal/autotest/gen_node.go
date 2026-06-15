package autotest

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

// NodeTestGenerator generates Jest tests for Node.js projects
type NodeTestGenerator struct {
	driver *ai.Driver
	logger *zap.Logger
}

// NewNodeTestGenerator creates a new Node test generator
func NewNodeTestGenerator(driver interface{}) *NodeTestGenerator {
	// driver is expected to be *ai.Driver
	var d *ai.Driver
	if v, ok := driver.(*ai.Driver); ok {
		d = v
	}
	return &NodeTestGenerator{
		driver: d,
		logger: zap.NewNop(),
	}
}

// Generate generates a Jest test file for a coverage gap
func (g *NodeTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	// Extract function/class info using regex
	pkg, snippet := extractNodeFunction(source, gap.Func)

	prompt := g.buildPrompt(pkg, gap.File, snippet)

	// Try JSON response first
	var out struct {
		Test string `json:"test"`
	}

	err := g.driver.Decide(ctx, prompt, &out)
	if err == nil && strings.TrimSpace(out.Test) != "" {
		content := []byte(stripFences(out.Test))
		return TestFile{
			Path:    nodeTestFilePath(gap.File),
			Content: content,
		}, nil
	}

	// Fallback: use raw completion
	resp, rerr := g.driver.Client().Complete(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	})
	if rerr != nil {
		return TestFile{}, fmt.Errorf("node gen: decide %w, fallback %w", err, rerr)
	}

	content := []byte(stripFences(resp.Content))
	return TestFile{
		Path:    nodeTestFilePath(gap.File),
		Content: content,
	}, nil
}

// buildPrompt creates the prompt for test generation
func (g *NodeTestGenerator) buildPrompt(pkg, file, snippet string) string {
	return fmt.Sprintf(`You are a Jest test author. Emit a single complete .test.js file using describe/it blocks.
Use modern JavaScript (ES6+) and Jest best practices.
Output ONLY the JavaScript source, no markdown fences.

Write a Jest test file for this code.

File: %s
Package: %s

Source code:
%s

Return a complete .test.js file with proper imports, describe blocks, and test cases.
Include edge cases and error handling where appropriate.`, file, pkg, snippet)
}

// SetLogger sets the logger for the generator
func (g *NodeTestGenerator) SetLogger(logger *zap.Logger) {
	g.logger = logger
}

// nodeTestFilePath returns the test file path for a source file
func nodeTestFilePath(file string) string {
	dir := filepath.Dir(file)
	base := filepath.Base(file)

	// Try common extensions in order
	for _, ext := range []string{".jsx", ".tsx", ".ts", ".js"} {
		if strings.HasSuffix(base, ext) {
			name := strings.TrimSuffix(base, ext)
			return filepath.Join(dir, name+".test"+ext)
		}
	}

	// Fallback: just append .test.js
	return filepath.Join(dir, base+".test.js")
}

// extractNodeFunction extracts a function/class from source using regex patterns
// For phase 1, we use simplified regex extraction (no Babel dependency)
func extractNodeFunction(source []byte, funcName string) (string, string) {
	src := string(source)

	// Try to extract the specific function/class mentioned in funcName
	// funcName format: "filename.js:L42" or just "functionName"

	// Extract line number if present
	lineNum := 0
	if strings.Contains(funcName, ":L") {
		parts := strings.Split(funcName, ":L")
		if len(parts) == 2 {
			if n, err := parseIntOrZero(parts[1]); err == nil {
				lineNum = n
			}
			funcName = parts[0]
		}
	}

	// Extract filename from funcName for package name
	pkg := funcName
	if strings.Contains(funcName, ".js") {
		pkg = filepath.Base(funcName)
	}

	// If we have a line number, extract around that line
	if lineNum > 0 {
		lines := strings.Split(src, "\n")
		if lineNum <= len(lines) {
			// Extract 20 lines before and after the target line
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

	// Fallback: return the entire source
	return pkg, src
}

// Node function extraction patterns for future use
var nodePatterns = []struct {
	pattern string
	name    string
}{
	// Named exports
	{`export\s+(?:async\s+)?function\s+(\w+)`, "function"},
	{`export\s+class\s+(\w+)`, "class"},
	{`export\s+(?:const|let|var)\s+(\w+)\s*=\s*(?:async\s+)?(?:function|\([^)]*\)\s*=>)`, "arrow-function"},

	// Default exports
	{`export\s+default\s+(?:async\s+)?function\s+(\w+)`, "default-function"},
	{`export\s+default\s+class\s+(\w+)`, "default-class"},
	{`export\s+default\s+(?:async\s+)?(?:function|\([^)]*\)\s*=>)`, "default-arrow"},
}

// matchNodeFunctions uses regex to find exported functions/classes
func matchNodeFunctions(source string) []FunctionInfo {
	var functions []FunctionInfo

	lines := strings.Split(source, "\n")
	for _, line := range lines {
		for _, pat := range nodePatterns {
			re := regexp.MustCompile(pat.pattern)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				functions = append(functions, FunctionInfo{
					Name: matches[1],
					Type: pat.name,
				})
			}
		}
	}

	return functions
}

// FunctionInfo represents a detected function or class
type FunctionInfo struct {
	Name string
	Type string
}

// parseIntOrZero safely parses an int, returning 0 on error
func parseIntOrZero(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

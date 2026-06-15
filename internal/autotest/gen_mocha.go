package autotest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

// MochaTestGenerator generates Mocha tests for Node.js projects
type MochaTestGenerator struct {
	driver *ai.Driver
	logger *zap.Logger
}

// NewMochaTestGenerator creates a new Mocha test generator
func NewMochaTestGenerator(driver interface{}) *MochaTestGenerator {
	var d *ai.Driver
	if v, ok := driver.(*ai.Driver); ok {
		d = v
	}
	return &MochaTestGenerator{
		driver: d,
		logger: zap.NewNop(),
	}
}

// Generate generates a Mocha test file for a coverage gap
func (g *MochaTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	// Extract function/class info using regex (reuse Node logic)
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
			Path:    MochaTestFilePath(gap.File, ""),
			Content: content,
		}, nil
	}

	// Fallback: use raw completion
	resp, rerr := g.driver.Client().Complete(ctx, llm.Request{
		Messages: []llm.Message{{Role: "user", Content: prompt}},
	})
	if rerr != nil {
		return TestFile{}, fmt.Errorf("mocha gen: decide %w, fallback %w", err, rerr)
	}

	content := []byte(stripFences(resp.Content))
	return TestFile{
		Path:    MochaTestFilePath(gap.File, ""),
		Content: content,
	}, nil
}

// buildPrompt creates the prompt for Mocha test generation
func (g *MochaTestGenerator) buildPrompt(pkg, file, snippet string) string {
	return `You are a Mocha test author. Emit a single complete .test.js file using describe/it blocks.
Use modern JavaScript (ES6+) and Mocha best practices.
Use the Node.js assert library for assertions (assert.equal, assert.strictEqual, assert.deepStrictEqual, etc.).
Include proper error handling tests.
Output ONLY the JavaScript source, no markdown fences.

Write a Mocha test file for this code.

File: ` + file + `
Package: ` + pkg + `

Source code:
` + snippet + `

Return a complete .test.js file with:
- Proper require/import statements
- describe blocks grouping related tests
- it blocks for individual test cases
- assert assertions (not expect)
- Edge cases and error handling
- Clear test descriptions`
}

// SetLogger sets the logger for the generator
func (g *MochaTestGenerator) SetLogger(logger *zap.Logger) {
	g.logger = logger
}

// MochaTestFilePath returns the test file path for a source file
// Supports intelligent detection of test/ directory vs same-directory organization
func MochaTestFilePath(sourceFile string, projectDir string) string {
	// If projectDir is provided, check for test/ directory
	if projectDir != "" {
		testDir := filepath.Join(projectDir, "test")
		if _, err := os.Stat(testDir); err == nil {
			// Use test/ directory mode
			relPath, _ := filepath.Rel(projectDir, sourceFile)
			base := filepath.Base(relPath)

			// Trim extension
			for _, ext := range []string{".jsx", ".tsx", ".ts", ".js"} {
				if strings.HasSuffix(base, ext) {
					base = strings.TrimSuffix(base, ext)
					break
				}
			}

			return filepath.Join(testDir, base+".test.js")
		}
	}

	// Same-directory mode (consistent with Jest)
	dir := filepath.Dir(sourceFile)
	base := filepath.Base(sourceFile)

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

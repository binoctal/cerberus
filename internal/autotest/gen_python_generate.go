package autotest

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/llm"
)

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

package autotest

import (
	"context"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/llm"
)

func (g *NodeTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	pkg, snippet := extractNodeFunction(source, gap.Func)
	prompt := g.buildPrompt(pkg, gap.File, snippet)

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

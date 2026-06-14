package autotest

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

type GoTestGenerator struct {
	driver *ai.Driver
	logger *zap.Logger
}

func NewGoTestGenerator(driver *ai.Driver, logger *zap.Logger) *GoTestGenerator {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GoTestGenerator{driver: driver, logger: logger}
}

// Generate extracts the target function's signature+body from source, asks the
// LLM for a table-driven test, returns it as a TestFile named after the source.
func (g *GoTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
	pkg, snippet := extractFunc(source, gap.Func)
	prompt := ai.NewPrompt().
		System("You are a Go test author. Emit a single complete _test.go file. " +
			"Use table-driven tests. Output ONLY the Go source, no markdown fences.").
		Task(fmt.Sprintf("Write a Go test file for this function.\n\nPackage: %s\nFunction source:\n%s\n\n"+
			"Return a complete _test.go file with package declaration, imports, and table-driven tests.", pkg, snippet)).
		Build()

	// Prefer a JSON-shaped response {test: "..."}; fall back to raw completion.
	var out struct {
		Test string `json:"test"`
	}
	if err := g.driver.Decide(ctx, prompt, &out); err != nil || strings.TrimSpace(out.Test) == "" {
		// Fallback: call the underlying client directly for raw text
		resp, rerr := g.driver.Client().Complete(ctx, llm.Request{
			Messages: []llm.Message{{Role: "user", Content: prompt}},
		})
		if rerr != nil {
			return TestFile{}, fmt.Errorf("autotest gen: decide %w, fallback %w", err, rerr)
		}
		out.Test = resp.Content
	}
	content := []byte(stripFences(out.Test))
	return TestFile{Path: testFilePath(gap.File), Content: content}, nil
}

// extractFunc parses source and returns (packageName, source-snippet of gap.Func).
func extractFunc(source []byte, funcName string) (string, string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", source, parser.ParseComments)
	if err != nil {
		return "", string(source)
	}
	pkg := f.Name.Name
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name == nil || fd.Name.Name != funcName {
			continue
		}
		start, end := fset.Position(fd.Pos()).Offset, fset.Position(fd.End()).Offset
		if end <= len(source) {
			return pkg, string(source[start:end])
		}
	}
	return pkg, string(source)
}

func testFilePath(src string) string {
	// Preserve the source directory: internal/llm/claude.go → internal/llm/claude_test.go
	return strings.TrimSuffix(src, ".go") + "_test.go"
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```go")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

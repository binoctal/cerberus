package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
)

func TestGoTestGenerator_Generate(t *testing.T) {
	body := "package p\n\nfunc TestAdd(t *testing.T){ if Add(1,2)!=3{t.Fail()} }\n"
	mock := llm.NewMockClient(map[string]string{"default": body})
	driver := ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))

	g := NewGoTestGenerator(driver, zap.NewNop())
	src := []byte("package p\n\n// Add sums two ints.\nfunc Add(a, b int) int { return a + b }\n")
	tf, err := g.Generate(context.Background(), CoverageGap{File: "a.go", Func: "Add"}, src)
	require.NoError(t, err)
	assert.Equal(t, "a_test.go", tf.Path)
	assert.Contains(t, string(tf.Content), "package p")
	assert.Contains(t, string(tf.Content), "TestAdd")
}

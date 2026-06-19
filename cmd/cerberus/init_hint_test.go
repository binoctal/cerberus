package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProxyHint_NoneWhenNoEndpoint(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "")
	assert.Empty(t, proxyHint())
}

func TestProxyHint_NoneForOfficialEndpoint(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "https://api.anthropic.com")
	assert.Empty(t, proxyHint())
}

func TestProxyHint_WarnsForCustomEndpoint(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "https://open.bigmodel.cn/api/anthropic")
	hint := proxyHint()
	assert.Contains(t, hint, "custom LLM endpoint")
	assert.Contains(t, hint, "ai_budget.model")
}

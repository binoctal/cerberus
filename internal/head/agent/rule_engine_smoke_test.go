package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/types"
)

// TestRuleEngine_SmokeJudgment: a FallbackFor (smoke) case is judged by status
// code alone — 2xx/3xx/4xx pass, 5xx fails — and never depends on the normal
// OK (2xx/3xx) rule, so a 404 still recovers.
func TestRuleEngine_SmokeJudgment(t *testing.T) {
	pass := func(status int) bool {
		r := types.HTTPResult{OK: status >= 200 && status < 400, StatusCode: status}
		return smokePasses(r)
	}
	assert.True(t, pass(200), "2xx passes")
	assert.True(t, pass(301), "3xx passes")
	assert.True(t, pass(404), "4xx passes (reachable, non-5xx)")
	assert.False(t, pass(500), "5xx fails")
	assert.False(t, pass(503), "5xx fails")
	assert.False(t, smokePasses(types.HTTPResult{OK: false, StatusCode: 0, Err: "dial: connection refused"}),
		"transport error (status 0) fails")
}

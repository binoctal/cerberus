package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResolvePlaceholdersPad pins the {{pad:N}} intrinsic (oversize-payload
// negative cases): N filler bytes rendered inline, non-positive N is an
// error, and the widening of the placeholder regex to admit ':' must not
// disturb role/param resolution.
func TestResolvePlaceholdersPad(t *testing.T) {
	out, err := resolvePlaceholders(nil, nil, "", `{"type":"chat:message","payload":{"text":"{{pad:100}}"}}`)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, `{"type":"chat:message","payload":{"text":"xxxx`))
	assert.True(t, strings.HasSuffix(out, `"}}`))
	assert.Len(t, strings.TrimSuffix(strings.TrimPrefix(out, `{"type":"chat:message","payload":{"text":"`), `"}}`), 100)
}

func TestResolvePlaceholdersPadZeroIsError(t *testing.T) {
	_, err := resolvePlaceholders(nil, nil, "", "{{pad:0}}")
	assert.ErrorContains(t, err, "unresolved placeholder {{pad:0}}")
}

func TestResolvePlaceholdersPadBadNumberIsError(t *testing.T) {
	_, err := resolvePlaceholders(nil, nil, "", "{{pad:abc}}")
	assert.ErrorContains(t, err, "unresolved placeholder {{pad:abc}}")
}

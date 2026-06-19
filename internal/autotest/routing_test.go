package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectLanguage(t *testing.T) {
	assert.Equal(t, "go", detectLanguage("test/fixtures/go-lib/math.go", nil))
	assert.Equal(t, "node", detectLanguage("test/fixtures/node-app/lib.js", map[string]bool{"package.json": true}))
	assert.Equal(t, "python", detectLanguage("test/fixtures/python-pkg/math.py", map[string]bool{"requirements.txt": true}))
	assert.Equal(t, "python", detectLanguage("test/fixtures/python-pkg/app.py", map[string]bool{"pyproject.toml": true}))
	assert.Equal(t, "node", detectLanguage("test/fixtures/ts-app/components.ts", map[string]bool{"package.json": true}))
	assert.Equal(t, "go", detectLanguage("unknown/path.txt", nil)) // default fallback
}

func TestSelectProvider(t *testing.T) {
	// detectLanguage → provider name (string, not concrete type — factory resolves later)
	assert.Equal(t, "go", providerForLanguage("go"))
	assert.Equal(t, "node", providerForLanguage("node"))
	assert.Equal(t, "python", providerForLanguage("python"))
	assert.Equal(t, "go", providerForLanguage("unknown")) // default fallback
}

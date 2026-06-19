package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectLanguage(t *testing.T) {
	assert.Equal(t, "go", DetectLanguage("test/fixtures/go-lib/math.go", nil))
	assert.Equal(t, "node", DetectLanguage("test/fixtures/node-app/lib.js", map[string]bool{"package.json": true}))
	assert.Equal(t, "python", DetectLanguage("test/fixtures/python-pkg/math.py", map[string]bool{"requirements.txt": true}))
	assert.Equal(t, "python", DetectLanguage("test/fixtures/python-pkg/app.py", map[string]bool{"pyproject.toml": true}))
	assert.Equal(t, "node", DetectLanguage("test/fixtures/ts-app/components.ts", map[string]bool{"package.json": true}))
	assert.Equal(t, "go", DetectLanguage("unknown/path.txt", nil)) // default fallback
}

func TestSelectProvider(t *testing.T) {
	// DetectLanguage → provider name (string, not concrete type — factory resolves later)
	assert.Equal(t, "go", ProviderForLanguage("go"))
	assert.Equal(t, "node", ProviderForLanguage("node"))
	assert.Equal(t, "python", ProviderForLanguage("python"))
	assert.Equal(t, "go", ProviderForLanguage("unknown")) // default fallback
}

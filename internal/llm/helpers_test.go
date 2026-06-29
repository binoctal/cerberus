package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeModelID(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"lowercase passthrough", "claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"mixed case lowered", "GLM-4.7", "glm-4.7"},
		{"thinking suffix stripped", "glm-5.2[1m]", "glm-5.2"},
		{"uppercase plus suffix", "GLM-5.2[1M]", "glm-5.2"},
		{"multi-digit budget", "glm-5.2[32m]", "glm-5.2"},
		{"suffix only at end", "glm[1m]-5.2", "glm[1m]-5.2"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeModelID(tc.model))
		})
	}
}

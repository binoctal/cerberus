package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const sampleClaimsYAML = `
source:
  files:
    - path: ../../../open-agents/README.md
      hash: sha256:abc123
claims:
  - id: schedule-real-cli
    text: "调度真实 AI CLI 执行任务"
    source_ref: "README.md:3"
    critical: true
    implies_cardinality: 1
  - id: desktop-notify
    text: "桌面通知"
    critical: false
    status_annotation: "no surface mapping"
`

func TestClaimsYAMLParse(t *testing.T) {
	var cf ClaimsFile
	require.NoError(t, yaml.Unmarshal([]byte(sampleClaimsYAML), &cf))
	require.Len(t, cf.Claims, 2)
	c := cf.Claims[0]
	assert.Equal(t, "schedule-real-cli", c.ID)
	assert.Equal(t, "调度真实 AI CLI 执行任务", c.Text)
	assert.Equal(t, "README.md:3", c.SourceRef)
	assert.True(t, c.Critical)
	assert.Equal(t, 1, c.ImpliesCardinality)
	assert.False(t, c.WontTest())

	n := cf.Claims[1]
	assert.False(t, n.Critical)
	assert.Equal(t, "no surface mapping", n.StatusAnnotation)
	assert.False(t, n.WontTest(), "no surface mapping is informational, not an exemption")
}

func TestClaimWontTest(t *testing.T) {
	assert.True(t, Claim{StatusAnnotation: "wont-test(proven by integration suite)"}.WontTest())
	assert.True(t, Claim{StatusAnnotation: "wont-test()"}.WontTest() == false, "empty reason is not an exemption")
	assert.False(t, Claim{}.WontTest())
	assert.False(t, Claim{StatusAnnotation: "maybe later"}.WontTest())
}

func TestValidateClaims(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"duplicate id", `
claims:
  - {id: a, text: "one"}
  - {id: a, text: "two"}
`, "duplicate claim id"},
		{"bad id", `
claims:
  - {id: "Not Valid", text: "x"}
`, "not a valid claim id"},
		{"empty text", `
claims:
  - {id: a, text: ""}
`, "text is required"},
		{"wont-test without reason", `
claims:
  - {id: a, text: "x", status_annotation: "wont-test"}
`, "needs a reason"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cf ClaimsFile
			require.NoError(t, yaml.Unmarshal([]byte(tc.yaml), &cf))
			err := ValidateClaims(&cf)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestValidateClaimsOK(t *testing.T) {
	var cf ClaimsFile
	require.NoError(t, yaml.Unmarshal([]byte(sampleClaimsYAML), &cf))
	assert.NoError(t, ValidateClaims(&cf))
}

func TestLoadClaims(t *testing.T) {
	t.Run("absent file returns nil nil", func(t *testing.T) {
		cf, err := LoadClaims(t.TempDir())
		require.NoError(t, err)
		assert.Nil(t, cf)
	})
	t.Run("present file loads and validates", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "claims.yaml"), []byte(sampleClaimsYAML), 0o644))
		cf, err := LoadClaims(dir)
		require.NoError(t, err)
		require.NotNil(t, cf)
		assert.Len(t, cf.Claims, 2)
	})
	t.Run("invalid file errors", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".cerberus", "claims.yaml"),
			[]byte("claims:\n  - {id: a, text: \"\"}"), 0o644))
		_, err := LoadClaims(dir)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "text is required") || strings.Contains(err.Error(), "claims.yaml"), err.Error())
	})
}

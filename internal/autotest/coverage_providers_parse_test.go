package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Jest/Istanbul share one JSON format: per-file statement counts (s) keyed by
// an index into statementMap, whose ranges carry start/end line numbers.
const jestFixture = `{
  "/src/foo.js": {
    "statementMap": {
      "0": {"start": {"line": 1}, "end": {"line": 2}},
      "1": {"start": {"line": 5}, "end": {"line": 6}}
    },
    "s": {"0": 1, "1": 0}
  }
}`

// Node provider parses Jest JSON: two statements, one covered → 1/2.
func TestNodeParseJestCoverage(t *testing.T) {
	p := NewNodeCoverageProvider(nil)
	rep, err := p.parseJestCoverage([]byte(jestFixture))
	require.NoError(t, err)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
	require.Len(t, rep.Profile, 2)
	assert.Equal(t, "/src/foo.js", rep.Profile[0].File)
	assert.Equal(t, 1, rep.Profile[0].Count) // covered
	assert.Equal(t, 0, rep.Profile[1].Count) // uncovered
}

// Node provider surfaces malformed JSON as an error.
func TestNodeParseJestCoverage_BadJSON(t *testing.T) {
	p := NewNodeCoverageProvider(nil)
	_, err := p.parseJestCoverage([]byte("{not json"))
	require.Error(t, err)
}

// Mocha provider parses Istanbul JSON, which is the same format as Jest.
func TestMochaParseIstanbulCoverage(t *testing.T) {
	p := NewMochaCoverageProvider(nil)
	rep, err := p.parseIstanbulCoverage([]byte(jestFixture))
	require.NoError(t, err)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

// Python provider: new coverage.py format with executed_lines/missing_lines.
func TestPythonParseJSONCoverage_NewFormat(t *testing.T) {
	p := NewPythonCoverageProvider(nil)
	data := []byte(`{"files":{"foo.py":{"executed_lines":[1,2],"missing_lines":[3]}}}`)
	rep, err := p.parseJSONCoverage(data)
	require.NoError(t, err)
	assert.Equal(t, 3, rep.TotalFuncs) // 2 executed + 1 missing
	assert.Equal(t, 2, rep.CoveredFuncs)
}

// Python provider: legacy format with a lines map (line number string → count).
func TestPythonParseJSONCoverage_LegacyFormat(t *testing.T) {
	p := NewPythonCoverageProvider(nil)
	data := []byte(`{"files":{"foo.py":{"lines":{"1":1,"2":0}}}}`)
	rep, err := p.parseJSONCoverage(data)
	require.NoError(t, err)
	assert.Equal(t, 2, rep.TotalFuncs)
	assert.Equal(t, 1, rep.CoveredFuncs)
}

// Python provider surfaces malformed JSON as an error.
func TestPythonParseJSONCoverage_BadJSON(t *testing.T) {
	p := NewPythonCoverageProvider(nil)
	_, err := p.parseJSONCoverage([]byte("{not json"))
	require.Error(t, err)
}

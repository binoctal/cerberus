package autotest

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafetyMode_Constants(t *testing.T) {
	assert.Equal(t, SafetyMode("approve"), SafetyApprove)
	assert.Equal(t, SafetyMode("auto"), SafetyAuto)
	assert.Equal(t, SafetyMode("dry-run"), SafetyDryRun)
}

func TestCoverageGap_Reasons(t *testing.T) {
	g := CoverageGap{File: "a.go", Func: "F", Reason: ReasonNoTestFile}
	assert.Equal(t, "no test file", g.Reason)
}

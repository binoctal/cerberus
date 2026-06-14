// Package autotest runs a project's tests with coverage, finds uncovered code,
// AI-generates tests to fill the gaps, and verifies them. It is the post-
// Examiner phase of `cerberus run`.
package autotest

import (
	"context"
	"time"
)

// SafetyMode controls how generated tests reach disk.
type SafetyMode string

const (
	SafetyApprove SafetyMode = "approve" // default: gate prompts before write
	SafetyAuto    SafetyMode = "auto"    // write directly, report after
	SafetyDryRun  SafetyMode = "dry-run" // report only, never write
)

// Reasons for a CoverageGap.
const (
	ReasonZeroCover  = "0% covered"
	ReasonNoTestFile = "no test file"
)

// CoverageLine is one covered span from a coverage profile.
type CoverageLine struct {
	File       string
	Start, End int
	Count      int
}

// CoverageReport is the output of RunCoverage.
type CoverageReport struct {
	Pass                     bool
	Profile                  []CoverageLine
	TotalFuncs, CoveredFuncs int
}

// CoverageGap is an uncovered target worth generating a test for.
type CoverageGap struct {
	File, Func string
	Reason     string
}

// TestFile is a generated test awaiting (possible) write.
type TestFile struct {
	Path    string
	Content []byte
}

// AutoTestReport is the phase output.
type AutoTestReport struct {
	Gaps              []CoverageGap
	Generated         []TestFile
	Written           []string
	Skipped, Failed   []string
	Reverted          []string
	BeforeCoveragePct float64
	AfterCoveragePct  float64
	Duration          time.Duration
}

// context import retained for interface signatures in provider.go.
var _ = context.Background

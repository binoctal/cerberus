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

// ProjectType represents the type of project (Go, Node, Mocha, Python)
type ProjectType string

// ProjectType constants for AutoTest providers.
const (
	ProjectTypeGo     ProjectType = "go"
	ProjectTypeNode   ProjectType = "node"  // Jest
	ProjectTypeMocha  ProjectType = "mocha" // Mocha + nyc
	ProjectTypePython ProjectType = "python"
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
	Content []byte `json:"-"` // Don't marshal to DB (dry-run stdout reads from memory)
}

// AutoTestItem represents a single gap's target and result.
type AutoTestItem struct {
	TargetFile string `json:"target_file"` // gap.File - source file being tested
	TargetFunc string `json:"target_func"` // gap.Func - function being tested
	Reason     string `json:"reason"`      // "0% covered" | "no test file" - why generated
	TestPath   string `json:"test_path"`   // generated _test.go path (empty if failed)
	Status     string `json:"status"`      // "written" | "reverted" | "skipped" | "failed" | "generated"
}

// AutoTestReport is the phase output.
type AutoTestReport struct {
	Gaps              []CoverageGap  `json:"gaps"`
	Generated         []TestFile     `json:"generated"`
	Written           []string       `json:"written"`
	Skipped           []string       `json:"skipped"`
	Failed            []string       `json:"failed"`
	Reverted          []string       `json:"reverted"`
	Items             []AutoTestItem `json:"items"` // Per-item aligned records (target + result)
	BeforeCoveragePct float64        `json:"before_coverage_pct"`
	AfterCoveragePct  float64        `json:"after_coverage_pct"`
	Duration          time.Duration  `json:"duration"`
}

// context import retained for interface signatures in provider.go.
var _ = context.Background

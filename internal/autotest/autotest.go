package autotest

import "go.uber.org/zap"

// AutoTest coordinates automated test generation and coverage verification
type AutoTest struct {
	coverage       CoverageProvider
	gen            TestGenerator
	gate           RequestGate
	writer         Writer
	mode           SafetyMode
	MaxGaps        int // cap on gaps generated per run (0 = unlimited); defaults to 5
	MaxConcurrency int // max parallel workers (0 = serial); defaults to 3
	logger         *zap.Logger

	// excludedTargets holds (File,Func) tuples already covered by the coverage
	// repair loop; Run drops matching discovered gaps so Phase 4 does not
	// regenerate tests for them (D1 spec §6.7). Keyed by File + "\x00" + Func.
	excludedTargets map[string]bool
}

package autotest

import "go.uber.org/zap"

// AutoTest coordinates automated test generation and coverage verification
type AutoTest struct {
	coverage       CoverageProvider
	gen            TestGenerator
	gate           RequestGate
	writer         Writer
	mode           SafetyMode
	MaxGaps        int  // cap on gaps generated per run (0 = unlimited); defaults to 5
	MaxConcurrency int  // max parallel workers (0 = serial); defaults to 3
	logger         *zap.Logger
}

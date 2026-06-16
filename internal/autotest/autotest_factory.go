package autotest

import "go.uber.org/zap"

// NewAutoTest creates a new AutoTest coordinator
func NewAutoTest(cov CoverageProvider, gen TestGenerator, gate RequestGate, w Writer, mode SafetyMode, logger *zap.Logger) *AutoTest {
	if logger == nil {
		logger = zap.NewNop()
	}
	if w == nil {
		w = FSWriter{}
	}
	return &AutoTest{
		coverage:       cov,
		gen:            gen,
		gate:           gate,
		writer:         w,
		mode:           mode,
		MaxGaps:        5,
		MaxConcurrency: 1, // default to serial for backward compatibility
		logger:         logger,
	}
}

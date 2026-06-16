package agent

import (
	"go.uber.org/zap"
)

// DefaultParallelConfig returns sensible defaults.
func DefaultParallelConfig() ParallelConfig {
	return ParallelConfig{MaxWorkers: 4}
}

// NewParallelExecutor creates a parallel test executor.
func NewParallelExecutor(loop *ReActLoop, config ParallelConfig, logger *zap.Logger) *ParallelExecutor {
	return &ParallelExecutor{loop: loop, config: config, logger: logger}
}

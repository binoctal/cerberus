package agent

import (
	"go.uber.org/zap"
)

// ParallelConfig controls parallel execution behavior.
type ParallelConfig struct {
	MaxWorkers int // Maximum concurrent workers (default: 4)
}

// ParallelExecutor runs independent test cases concurrently with dependency ordering.
type ParallelExecutor struct {
	loop   *ReActLoop
	config ParallelConfig
	logger *zap.Logger
}

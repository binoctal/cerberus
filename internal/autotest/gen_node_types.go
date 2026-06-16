package autotest

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

// NodeTestGenerator generates Jest tests for Node.js projects
type NodeTestGenerator struct {
	driver *ai.Driver
	logger *zap.Logger
}

// FunctionInfo represents a detected function or class
type FunctionInfo struct {
	Name string
	Type string
}

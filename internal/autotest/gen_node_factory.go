package autotest

import (
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
)

// NewNodeTestGenerator creates a new Node test generator
func NewNodeTestGenerator(driver interface{}) *NodeTestGenerator {
	var d *ai.Driver
	if v, ok := driver.(*ai.Driver); ok {
		d = v
	}
	return &NodeTestGenerator{
		driver: d,
		logger: zap.NewNop(),
	}
}

// SetLogger sets the logger for the generator
func (g *NodeTestGenerator) SetLogger(logger *zap.Logger) {
	g.logger = logger
}

package autotest

import (
	"go.uber.org/zap"
)

// NewMochaCoverageProvider creates a new Mocha coverage provider
func NewMochaCoverageProvider(cfg *CoverageConfig) *MochaCoverageProvider {
	return &MochaCoverageProvider{
		config: cfg,
		logger: zap.NewNop(),
	}
}

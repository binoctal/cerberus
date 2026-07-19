package autotest

import "go.uber.org/zap"

// MochaCoverageProvider implements CoverageProvider for Mocha + nyc projects
type MochaCoverageProvider struct {
	config *CoverageConfig
	run    coverageRunner
	logger *zap.Logger
}

// SetLogger sets the logger for the provider
func (p *MochaCoverageProvider) SetLogger(logger *zap.Logger) {
	p.logger = logger
}

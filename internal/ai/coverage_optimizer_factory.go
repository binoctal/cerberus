package ai

import (
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/pkg/business"
)

// NewCoverageOptimizer creates a new coverage optimizer
func NewCoverageOptimizer(llmClient llm.Client, businessModel *business.BusinessModel) *CoverageOptimizer {
	return &CoverageOptimizer{
		llmClient:        llmClient,
		businessModel:    businessModel,
		testRunner:       NewTestRunner(),
		coverageAnalyzer: NewCoverageAnalyzer(),
		gapAnalyzer:      NewGapAnalyzer(llmClient, businessModel),
		testGenerator:    NewAITestGenerator(businessModel, llmClient),
		maxIterations:    5,
		targetCoverage:   0.90, // Target 90%
	}
}

// NewTestRunner creates a new test runner
func NewTestRunner() *TestRunner {
	return &TestRunner{}
}

// NewCoverageAnalyzer creates a new coverage analyzer
func NewCoverageAnalyzer() *CoverageAnalyzer {
	return &CoverageAnalyzer{}
}

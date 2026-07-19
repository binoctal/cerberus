package autotest

import (
	"fmt"
)

// DetectProjectType automatically detects the project type
func DetectProjectType(projectDir string) (ProjectType, float64, error) {
	detectors := []ProjectDetector{
		&GoProjectDetector{},
		&NodeProjectDetector{},
		&MochaProjectDetector{},
		&PythonProjectDetector{},
	}

	var bestType ProjectType
	var bestConfidence float64

	for _, detector := range detectors {
		supported, confidence, _ := detector.Detect(projectDir)
		if supported && confidence > bestConfidence {
			bestType = detector.Type()
			bestConfidence = confidence
		}
	}

	if bestConfidence >= 0.9 {
		return bestType, bestConfidence, nil
	}

	return "", 0, fmt.Errorf("no supported project type detected (confidence threshold 0.9 not met)")
}

// CreateProvider creates a provider and generator for the detected project type
func CreateProvider(typ ProjectType, driver interface{}, cfg *CoverageConfig) (CoverageProvider, TestGenerator, error) {
	switch typ {
	case ProjectTypeGo:
		// Use existing Go provider (driver is *ai.Driver)
		// This will be handled by the existing code path
		return nil, nil, fmt.Errorf("go provider should use existing path")
	case ProjectTypeNode:
		return NewNodeCoverageProvider(cfg, DefaultNodeCoverageRunner, nil), NewNodeTestGenerator(driver), nil
	case ProjectTypeMocha:
		if cfg == nil {
			cfg = DefaultMochaCoverageConfig()
		}
		return NewMochaCoverageProvider(cfg), NewMochaTestGenerator(driver), nil
	case ProjectTypePython:
		return NewPythonCoverageProvider(cfg, DefaultPythonCoverageRunner, nil), NewPythonTestGenerator(driver), nil
	default:
		return nil, nil, fmt.Errorf("unsupported project type: %s", typ)
	}
}

// DetectAndCreateProvider auto-detects project type and creates appropriate provider
func DetectAndCreateProvider(projectDir string, driver interface{}) (CoverageProvider, TestGenerator, error) {
	typ, confidence, err := DetectProjectType(projectDir)
	if err != nil {
		return nil, nil, fmt.Errorf("detect project type: %w (confidence: %.2f)", err, confidence)
	}

	// Use default config for the detected type
	var cfg *CoverageConfig
	switch typ {
	case ProjectTypeNode:
		cfg = DefaultNodeCoverageConfig()
	case ProjectTypeMocha:
		cfg = DefaultMochaCoverageConfig()
	case ProjectTypePython:
		cfg = DefaultPythonCoverageConfig()
	default:
		cfg = &CoverageConfig{}
	}

	return CreateProvider(typ, driver, cfg)
}

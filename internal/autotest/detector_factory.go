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

package policy

import (
	"regexp"

	"github.com/binoctal/cerberus/internal/types"
)

// AnomalyDetector checks execution results for suspicious patterns.
type AnomalyDetector struct {
	maxOutputBytes    int
	sensitivePatterns []*regexp.Regexp
}

// NewDefaultAnomalyDetector creates an anomaly detector with default thresholds.
func NewDefaultAnomalyDetector() *AnomalyDetector {
	return &AnomalyDetector{
		maxOutputBytes: 1 << 20, // 1MB
		sensitivePatterns: []*regexp.Regexp{
			regexp.MustCompile(`(?i)password"?\s*[:=]`),
			regexp.MustCompile(`(?i)secret"?\s*[:=]`),
			regexp.MustCompile(`(?i)api[_-]?key"?\s*[:=]`),
			regexp.MustCompile(`-----BEGIN (RSA |EC )?PRIVATE KEY-----`),
		},
	}
}

// Check returns true if the result shows anomalous behavior.
func (d *AnomalyDetector) Check(result types.ExecutorResult) bool {
	evidence := result.Evidence()

	if len(evidence.Content) > d.maxOutputBytes {
		return true
	}

	for _, p := range d.sensitivePatterns {
		if p.MatchString(evidence.Content) {
			return true
		}
	}

	return false
}

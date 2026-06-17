package examiner

const (
	// MinimumStrategyLength is the minimum required length for reflection strategies.
	MinimumStrategyLength = 10

	// MaxResponseBodyLength is the maximum length for HTTP response bodies in reflection context.
	MaxResponseBodyLength = 500

	// ReflectionTypeFailure represents a failure reflection.
	ReflectionTypeFailure = "failure"

	// ReflectionTypeSuccess represents a success reflection.
	ReflectionTypeSuccess = "success"
)

// qualityGate validates a reflection before L3 storage.
// It checks that the reflection contains required fields and meets quality thresholds.
func qualityGate(r Reflection) bool {
	if r.Diagnosis == "" {
		return false
	}
	if len(r.Strategy) < MinimumStrategyLength {
		return false
	}
	if r.ConditionPattern == "" {
		return false
	}
	if r.Type != ReflectionTypeFailure && r.Type != ReflectionTypeSuccess {
		return false
	}
	return true
}

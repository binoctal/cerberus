package examiner

import "testing"

// classifyDrift sorts one judge verdict into one of four drift categories.
// Ground truth is pass for every validation case, so fail is the only incorrect
// verdict; uncertain is treated as honest (exclusion-gated claims are genuinely
// unproven until an active probe lands), and a low-confidence pass is
// under-confident. The asymmetry is intentional: an under-confident pass is an
// unreliable correct (the next run may flip it), while an honest-uncertain
// verdict is a reliable unknowable.
func classifyDrift(status JudgeStatus, conf, threshold float64) string {
	switch {
	case status == StatusFail:
		return "incorrect"
	case status == StatusUncertain:
		return "honest-uncertain"
	case status == StatusPass && conf < threshold:
		return "under-confident"
	default:
		return "clean"
	}
}

// TestClassifyDrift covers all four categories plus the boundary case where
// conf == threshold (clean, since the under-confident check is strict <).
func TestClassifyDrift(t *testing.T) {
	const th = 0.9
	tests := []struct {
		name   string
		status JudgeStatus
		conf   float64
		want   string
	}{
		{"incorrect is fail regardless of conf", StatusFail, 0.99, "incorrect"},
		{"honest-uncertain", StatusUncertain, 0.30, "honest-uncertain"},
		{"under-confident is pass below threshold", StatusPass, 0.80, "under-confident"},
		{"clean is pass at or above threshold", StatusPass, 0.95, "clean"},
		{"boundary conf==threshold is clean", StatusPass, th, "clean"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDrift(tc.status, tc.conf, th); got != tc.want {
				t.Errorf("classifyDrift(%s, %.2f, %.1f) = %q, want %q",
					tc.status, tc.conf, th, got, tc.want)
			}
		})
	}
}

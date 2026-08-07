package contract

import (
	"encoding/json"
	"testing"
)

func TestContractDefaults(t *testing.T) {
	c := Contract{Depth: "standard"}
	if c.Depth != "standard" {
		t.Fatalf("Depth = %q", c.Depth)
	}
	g := Gate{Module: "internal/llm", LineThreshold: 0.65}
	if g.LineThreshold != 0.65 {
		t.Fatalf("threshold = %v", g.LineThreshold)
	}
}

func TestAssessmentMeasuredDefaultsFalse(t *testing.T) {
	// Existing measured coverage paths set Measured=true explicitly; a zero-value
	// Assessment must not be misread as "measured". Callers gate on Measured.
	var a Assessment
	if a.Measured {
		t.Fatalf("zero-value Assessment.Measured must be false (not-applicable by default)")
	}
}

func TestGatePathThresholdRoundTrip(t *testing.T) {
	g := Gate{Module: "m", LineThreshold: 0.8, PathThreshold: 1.0}
	b, _ := json.Marshal(g)
	var got Gate
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PathThreshold != 1.0 {
		t.Fatalf("PathThreshold did not round-trip: %v", got.PathThreshold)
	}
}

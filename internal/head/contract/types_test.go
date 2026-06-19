package contract

import "testing"

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

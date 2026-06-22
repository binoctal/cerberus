package scout

import (
	"testing"
)

// Priority must never go negative: isDeprioritized uses Priority < 0 as its
// sentinel, so a >20-case ToT plan silently drops its tail cases.
func TestBestToPlanPriorityNeverNegative(t *testing.T) {
	descs := make([]string, 30)
	for i := range descs {
		descs[i] = "GET /endpoint"
	}
	plan := (&ToTPlanner{}).bestToPlan([]PlanCandidate{{Cases: descs}}, "g", "http://x")
	if len(plan.Cases) != 30 {
		t.Fatalf("expected 30 cases, got %d", len(plan.Cases))
	}
	for i, c := range plan.Cases {
		if c.Priority < 0 {
			t.Fatalf("case %d priority %v is negative — clashes with deprioritized sentinel",
				i, c.Priority)
		}
	}
}

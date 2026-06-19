package contract

import "testing"

func TestExpandDepth(t *testing.T) {
	smoke := ExpandDepth(DepthSmoke)
	if contains(smoke.PathTypes, "boundary") {
		t.Error("smoke must not include boundary paths")
	}
	std := ExpandDepth(DepthStandard)
	if !contains(std.PathTypes, "boundary") || !contains(std.ErrorScope, "4xx") {
		t.Error("standard must include boundary + 4xx")
	}
	thorough := ExpandDepth(DepthThorough)
	if !contains(thorough.Boundaries, "extreme") {
		t.Error("thorough must include extreme boundaries")
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

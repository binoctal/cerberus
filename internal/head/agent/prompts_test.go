package agent

import "testing"

func TestSteerPromptDocumentsWSPrimitives(t *testing.T) {
	for _, want := range []string{
		"ws_connect", "ws_send", "ws_receive", "ws_disconnect",
		"connection_id", "decisive",
	} {
		if !contains(promptSteerSystem, want) {
			t.Fatalf("steer prompt missing %q", want)
		}
	}
	if !contains(promptSteerSystem, "at most one decisive") {
		t.Fatal("steer prompt must state at-most-one-decisive")
	}
}

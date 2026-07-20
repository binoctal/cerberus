package types

import "testing"

func TestWSResultEvidenceIncludesMatchedAndSeen(t *testing.T) {
	r := WSResult{
		OK:            true,
		URL:           "ws://x/ws",
		MatchedMessage: `{"type":"permission:response","payload":{"approved":true}}`,
		SeenMessages:  []string{`{"type":"heartbeat"}`},
	}
	ev := r.Evidence()
	if ev.Type != "ws_messages" {
		t.Fatalf("evidence type = %s, want ws_messages", ev.Type)
	}
	if !contains(ev.Content, "permission:response") {
		t.Fatalf("evidence missing matched message: %s", ev.Content)
	}
	if !contains(ev.Content, "heartbeat") {
		t.Fatalf("evidence missing seen message: %s", ev.Content)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

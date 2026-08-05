package project

import (
	"strings"
	"testing"
)

func TestRenderVocabSummary(t *testing.T) {
	services := []Service{{
		Name: "realtime",
		Vocabulary: &Vocabulary{
			Source: VocabSource{ProtocolRef: "open-agents"},
			Edges: []VocabEdge{
				{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled",
					Delivery: VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress", Trigger: "message_handled",
					Delivery: VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled",
					Delivery: VocabDelivery{Mode: "send_bridge_by_device"}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "web", Type: "session:send", Trigger: "message_handled",
					Delivery: VocabDelivery{Mode: "broadcast_web", ExcludeSender: true}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "bridge", Type: "session:output", Trigger: "message_handled",
					Delivery: VocabDelivery{Mode: "broadcast_web"}, Partial: true},
				{FromRole: "bridge", ToRole: "web", Type: "device:offline", Trigger: "disconnect_bridge",
					Delivery: VocabDelivery{Mode: "broadcast_web"}},
			},
		},
	}}

	got := RenderVocabSummary(services)
	for _, want := range []string{
		"WS Routing Vocabulary (realtime, 6 edges)",
		"bridge->web broadcast_web (2):",
		"session:created",
		"workflow:task_progress",
		"web->bridge send_bridge_by_device[route=payload.deviceId] (1):",
		"session:start",
		"web->web broadcast_web(exclude_sender)[route=payload.deviceId] (1):",
		"session:send",
		"[skipped: 2 partial/unsupported/non-message_handled edges]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- summary ---\n%s", want, got)
		}
	}
}

func TestRenderVocabSummary_Empty(t *testing.T) {
	if got := RenderVocabSummary(nil); got != "" {
		t.Errorf("nil services = %q, want empty", got)
	}
	if got := RenderVocabSummary([]Service{{Name: "svc"}}); got != "" {
		t.Errorf("service without vocabulary = %q, want empty", got)
	}
}

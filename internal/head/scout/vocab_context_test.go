package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestRenderVocabSummary(t *testing.T) {
	services := []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{
			Source: project.VocabSource{ProtocolRef: "open-agents"},
			Edges: []project.VocabEdge{
				{FromRole: "bridge", ToRole: "web", Type: "session:created", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
				{FromRole: "web", ToRole: "bridge", Type: "session:start", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "send_bridge_by_device"}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "web", Type: "session:send", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web", ExcludeSender: true}, RouteField: "payload.deviceId"},
				{FromRole: "web", ToRole: "bridge", Type: "session:output", Trigger: "message_handled",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}, Partial: true},
				{FromRole: "bridge", ToRole: "web", Type: "device:offline", Trigger: "disconnect_bridge",
					Delivery: project.VocabDelivery{Mode: "broadcast_web"}},
			},
		},
	}}

	got := renderVocabSummary(services)
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
	if got := renderVocabSummary(nil); got != "" {
		t.Errorf("nil services = %q, want empty", got)
	}
	if got := renderVocabSummary([]project.Service{{Name: "svc"}}); got != "" {
		t.Errorf("service without vocabulary = %q, want empty", got)
	}
}

func TestBuildPlanningContextIncludesVocab(t *testing.T) {
	s := &Scout{config: &project.Config{Services: []project.Service{{
		Name: "realtime",
		Vocabulary: &project.Vocabulary{
			Source: project.VocabSource{ProtocolRef: "open-agents"},
			Edges: []project.VocabEdge{{
				FromRole: "bridge", ToRole: "web", Type: "workflow:task_progress", Trigger: "message_handled",
				Delivery: project.VocabDelivery{Mode: "broadcast_web"},
			}},
		},
	}}}}
	// buildPlanningContext -> buildPlanContext derefs model.API, so pass a
	// non-nil (empty) model rather than nil to avoid a panic.
	ctx := s.buildPlanningContext(&project.ProjectModel{}, "")
	if !strings.Contains(ctx, "WS Routing Vocabulary") ||
		!strings.Contains(ctx, "workflow:task_progress") {
		t.Errorf("planning context missing vocab summary\n--- context ---\n%s", ctx)
	}
}

func TestToTProposeTaskIncludesVocab(t *testing.T) {
	planner := &ToTPlanner{config: ToTConfig{GenerateN: 3}}
	planner.SetVocabSummary("\n\n## WS Routing Vocabulary (realtime, 1 edges)\nbridge->web broadcast_web (1): workflow:task_progress\n")
	task := planner.buildProposeTask(PlanCandidate{Description: "seed"}, &project.ProjectModel{}, "cover relay")
	if !strings.Contains(task, "WS Routing Vocabulary") ||
		!strings.Contains(task, "workflow:task_progress") {
		t.Errorf("ToT propose task missing vocab summary\n--- task ---\n%s", task)
	}
}

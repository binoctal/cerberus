package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

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

package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
)

// firstStepWithURL returns the first step whose URL contains substr, or the
// zero TestStep when none matches.
func firstStepWithURL(steps []agent.TestStep, substr string) agent.TestStep {
	for _, s := range steps {
		if strings.Contains(s.URL, substr) {
			return s
		}
	}
	return agent.TestStep{}
}

func TestMissionSeedCases_SetupChainOrder(t *testing.T) {
	// The planner provider step is env-gated; pin the env so the chain is the
	// full 6-step form deterministically.
	t.Setenv("CERBERUS_PLANNER_API_KEY", "test-key")
	t.Setenv("CERBERUS_PLANNER_API_URL", "https://planner.example")
	t.Setenv("CERBERUS_PLANNER_MODEL", "planner-model")
	cases := missionSeedCases(missionSendFixture(), map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 mission case, got %d", len(cases))
	}
	steps := cases[0].Steps
	// 0 user id capture, 1 plan seed, 2 user plan update, 3 provider,
	// 4 agent seed, 5 mission create. The /api/auth/me step leads because the
	// admin actor carries no statically-known user id (ruling: capture it).
	wantURLs := []string{
		"/api/auth/me", "/api/admin/billing/plans", "/api/admin/users/", "/api/admin/ai-providers",
		"/api/agents", "/api/missions",
	}
	idx := 0
	for _, s := range steps {
		if s.Action != "http_request" {
			continue
		}
		if idx >= len(wantURLs) || !strings.Contains(s.URL, wantURLs[idx]) {
			t.Fatalf("http step %d URL %q, want prefix %q", idx, s.URL, wantURLs[idx])
		}
		idx++
	}
	if idx != len(wantURLs) {
		t.Fatalf("want %d http steps, got %d", len(wantURLs), idx)
	}
	// Plan payload must NOT set max_concurrent_tasks (spec §2: the 0-trap).
	planStep := firstStepWithURL(steps, "/api/admin/billing/plans")
	if strings.Contains(planStep.Body, "max_concurrent_tasks") {
		t.Fatal("plan payload must omit max_concurrent_tasks")
	}
	if !strings.Contains(planStep.Body, `"workflows":true`) || !strings.Contains(planStep.Body, "daily_missions") {
		t.Fatal("plan payload must gate workflows + raise daily_missions")
	}
	// Read-back wiring: plan step captures the id; user step substitutes it.
	if planStep.Capture["id"] != "planId" {
		t.Fatalf("plan step capture = %v", planStep.Capture)
	}
	userStep := firstStepWithURL(steps, "/api/admin/users/")
	if !strings.Contains(userStep.URL, "{{case.planId}}") && !strings.Contains(userStep.Body, "{{case.planId}}") {
		t.Fatal("user plan step must consume {{case.planId}}")
	}
	// The user id is captured (not statically known) and consumed by the same
	// step's URL path.
	meStep := firstStepWithURL(steps, "/api/auth/me")
	if meStep.Capture["id"] != "userId" {
		t.Fatalf("auth/me step capture = %v", meStep.Capture)
	}
	if !strings.Contains(userStep.URL, "{{case.userId}}") {
		t.Fatal("user plan step URL must consume {{case.userId}}")
	}
	// The mission id is captured for later substitution (dot path: the create
	// route returns {mission:{id}}).
	missionStep := firstStepWithURL(steps, "/api/missions")
	if missionStep.Capture["mission.id"] != "missionId" {
		t.Fatalf("mission step capture = %v", missionStep.Capture)
	}
}

func TestMissionSeedCases_ReceiveWindow(t *testing.T) {
	cases := missionSeedCases(missionSendFixture(), map[string]bool{"bridge": true})
	var recvTimeouts []int
	for _, s := range cases[0].Steps {
		if s.Action == "ws_receive" {
			recvTimeouts = append(recvTimeouts, s.Timeout)
		}
	}
	// Deterministic pushes get MINUTE-SCALE windows (default 10s fails them).
	for _, to := range recvTimeouts {
		if to < 120 {
			t.Fatalf("receive timeout %d < 120s", to)
		}
	}
	// The completion signal is job_status (out-of-band), never job_completed.
	for _, s := range cases[0].Steps {
		if s.Action == "ws_receive" && s.Type == "workflow:job_completed" {
			t.Fatal("job_completed is a dead type; completion is workflow:job_status")
		}
	}
}

func TestMissionSeedCases_NoBridgeReal_EmitsNothing(t *testing.T) {
	if got := missionSeedCases(missionSendFixture(), nil); got != nil {
		t.Fatalf("emitted %d cases without a real bridge", len(got))
	}
}

package scout

import (
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// missionSeedFixture augments the shared send fixture the way the real
// dogfood project is shaped: a credentialed web (mission-user) role plus
// the vocabulary HTTP role map — mission-seed resolves its admin/web roles
// through that map, not through Go literals (the isAdminPath lesson).
func missionSeedFixture() project.Service {
	svc := missionSendFixture()
	svc.Protocol.Roles["web"] = &project.ProtocolRole{
		Params:        map[string]string{"type": "web"},
		CredentialRef: "web-actor",
	}
	svc.Vocabulary.HTTPRoleRoutes = []project.VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}}
	svc.Vocabulary.HTTPDefaultRole = "web"
	return svc
}

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
	cases := missionSeedCases(missionSeedFixture(), map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 mission case, got %d", len(cases))
	}
	steps := cases[0].Steps
	// 0 user id capture, 1 plan seed, 2 user plan update, 3 provider,
	// 4 agent seed, 5 mission create, 6 failing-mission create (the failure
	// path: retry exhaustion -> workflow:task_failed), 7 question-mission
	// create (human-in-the-loop: task_question -> task_answer). The
	// /api/auth/me step leads because the admin actor carries no
	// statically-known user id (ruling: capture it).
	wantURLs := []string{
		"/api/auth/me", "/api/admin/billing/plans", "/api/admin/users/", "/api/admin/ai-providers",
		"/api/agents", "/api/missions", "/api/missions", "/api/missions",
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
	// Plan payload must set max_concurrent_tasks POSITIVE (spec §2 0-trap:
	// 0 bricks dispatch). It cannot be omitted anymore — the admin write
	// path rejects partial limits sections (open-agents known-issue #12).
	planStep := firstStepWithURL(steps, "/api/admin/billing/plans")
	if !strings.Contains(planStep.Body, `"max_concurrent_tasks":100`) {
		t.Fatal("plan payload must set max_concurrent_tasks to a positive value")
	}
	if !strings.Contains(planStep.Body, `"workflows":true`) || !strings.Contains(planStep.Body, "daily_missions") {
		t.Fatal("plan payload must gate workflows + raise daily_missions")
	}
	// The route sweep's admin writes exhaust the fallback api_hourly (100)
	// within the hour; the seeded plan must lift both api counters or the
	// mission-setup POSTs 429 before any orchestration starts. max_agents
	// must be lifted too (fallback 5) or agent-row creation 403s after a
	// handful of repeat runs.
	if !strings.Contains(planStep.Body, "api_hourly") || !strings.Contains(planStep.Body, "api_daily") {
		t.Fatal("plan payload must lift api_hourly + api_daily")
	}
	if !strings.Contains(planStep.Body, "max_agents") {
		t.Fatal("plan payload must lift resources.max_agents")
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
	// User-scoped steps run under the WEB role: open-agents scopes device
	// ownership + room broadcast per user (checkDeviceOnline requires
	// devices.user_id = mission user), and only the web user owns the bridges.
	// Admin routes (plan seed, user switch, provider) stay under admin.
	for _, s := range steps {
		if s.Action != "http_request" {
			continue
		}
		isAdminRoute := strings.Contains(s.URL, "/api/admin/")
		if isAdminRoute && s.AuthRole != "admin" {
			t.Fatalf("admin route %q must use admin role, got %q", s.URL, s.AuthRole)
		}
		if !isAdminRoute && s.AuthRole != "web" {
			t.Fatalf("user-scoped route %q must use web role, got %q", s.URL, s.AuthRole)
		}
	}
}

func TestMissionSeedCases_ReceiveWindow(t *testing.T) {
	cases := missionSeedCases(missionSeedFixture(), map[string]bool{"bridge": true})
	var recvTimeouts []int
	for _, s := range cases[0].Steps {
		if s.Action == "ws_receive" {
			recvTimeouts = append(recvTimeouts, s.Timeout)
		}
	}
	// Deterministic pushes get MINUTE-SCALE windows (default 10s fails them:
	// decompose + orchestrator alarms + ACP connect timeout are all upstream).
	for _, to := range recvTimeouts {
		if to < 60 {
			t.Fatalf("receive timeout %d < 60s", to)
		}
	}
	// The orchestration path pushes task_progress (not task_started — that is
	// only the echo of web-origin sends) and never the dead job_completed.
	// Since the open-agents callback fix (fix/workflow-callback-url,
	// 2026-08-19) completion IS observable: task_completed + job_status via
	// the bridge HTTP callback -> DO broadcast, and task_result as the
	// bridge's reply to a web-initiated task_merge.
	for _, s := range cases[0].Steps {
		if s.Action != "ws_receive" {
			continue
		}
		switch s.Type {
		case "workflow:job_completed", "workflow:task_started":
			t.Fatalf("receive of %s is not emitted on the orchestration path (live-verified 2026-08-18)", s.Type)
		}
	}
}

func TestMissionSeedCases_NoBridgeReal_EmitsNothing(t *testing.T) {
	if got := missionSeedCases(missionSeedFixture(), nil); got != nil {
		t.Fatalf("emitted %d cases without a real bridge", len(got))
	}
}

// TestMissionSeedCases_FanoutMission: when ALL THREE bridge-family roles are
// real processes, a second case seeds a MULTI-DEVICE mission (deviceIds
// spanning the three captured bridges). The per-task device-targeted
// task_assign never reaches web spectators (room.ts routes it to the owning
// bridge only), so fan-out is observed through the bridge-origin
// workflow:task_progress frames, each carrying its executor's deviceId.
func TestMissionSeedCases_FanoutMission(t *testing.T) {
	roles := map[string]bool{"bridge": true, "bridge2": true, "bridge3": true}
	cases := missionSeedCases(missionSeedFixture(), roles)
	var fanout *agent.TestCase
	for i := range cases {
		if cases[i].ID == wsCaseID("open-agents", "wf", "mission-fanout") {
			fanout = &cases[i]
		}
	}
	if fanout == nil {
		t.Fatal("expected a mission-fanout case with all three bridges real")
	}
	if len(fanout.Claims) != 1 || fanout.Claims[0] != "multi-device-orchestration" {
		t.Fatalf("fanout claims = %v, want [multi-device-orchestration]", fanout.Claims)
	}
	create := firstStepWithURL(fanout.Steps, "/api/missions")
	if !strings.Contains(create.Body, `"{{bridge.deviceId}}"`) ||
		!strings.Contains(create.Body, `"{{bridge2.deviceId}}"`) ||
		!strings.Contains(create.Body, `"{{bridge3.deviceId}}"`) {
		t.Fatalf("fanout mission must address all three bridges: %s", create.Body)
	}
	if !strings.Contains(create.Body, `"autoConfirm":true`) {
		t.Fatal("fanout mission must auto-confirm")
	}
	progress := 0
	completed, jobStatus := false, false
	for _, s := range fanout.Steps {
		if s.Action != "ws_receive" {
			continue
		}
		switch s.Type {
		case "workflow:task_progress":
			progress++
		case "workflow:task_completed":
			completed = true
		case "workflow:job_status":
			jobStatus = true
		}
	}
	if progress < 3 {
		t.Fatalf("fanout case needs >= 3 task_progress receives (one per subtask), got %d", progress)
	}
	if !completed || !jobStatus {
		t.Fatal("fanout case must observe completion (task_completed + job_status)")
	}
}

// The fan-out case is emitted only when every bridge-family role is a real
// process — a two-bridge project keeps the single-device mission-seed shape.
func TestMissionSeedCases_FanoutNeedsAllThreeBridges(t *testing.T) {
	cases := missionSeedCases(missionSeedFixture(), map[string]bool{"bridge": true, "bridge2": true})
	for _, c := range cases {
		if c.ID == wsCaseID("open-agents", "wf", "mission-fanout") {
			t.Fatal("fanout case must not emit without bridge3")
		}
	}
}

// TestMissionSeedCases_RolesFromVocab: the admin/web role split is resolved
// through the vocabulary's HTTP role map, not through Go literals — a SUT
// that names its roles differently needs only a vocab edit (the isAdminPath
// lesson applied to the mission-seed generator).
func TestMissionSeedCases_RolesFromVocab(t *testing.T) {
	svc := missionSeedFixture()
	svc.Protocol.Roles["root"] = &project.ProtocolRole{CredentialRef: "root-actor"}
	svc.Protocol.Roles["enduser"] = &project.ProtocolRole{CredentialRef: "enduser-actor"}
	svc.Vocabulary.HTTPRoleRoutes = []project.VocabRoleRoute{{Prefix: "/api/admin", Role: "root"}}
	svc.Vocabulary.HTTPDefaultRole = "enduser"
	cases := missionSeedCases(svc, map[string]bool{"bridge": true})
	if len(cases) != 1 {
		t.Fatalf("want exactly 1 mission case, got %d", len(cases))
	}
	for _, s := range cases[0].Steps {
		if s.Action != "http_request" {
			continue
		}
		isAdminRoute := strings.Contains(s.URL, "/api/admin/")
		if isAdminRoute && s.AuthRole != "root" {
			t.Fatalf("admin route %q must use the vocab-mapped root role, got %q", s.URL, s.AuthRole)
		}
		if !isAdminRoute && s.AuthRole != "enduser" {
			t.Fatalf("user-scoped route %q must use the vocab default enduser role, got %q", s.URL, s.AuthRole)
		}
	}
	// The web observer connection binds the same vocab-resolved mission-user
	// role (device ownership and room broadcast scope to that user).
	for _, s := range cases[0].Steps {
		if s.Action == "ws_connect" && s.Role != "enduser" {
			t.Fatalf("ws_connect role = %q, want enduser", s.Role)
		}
	}
}

// TestMissionSeedCases_NoMissionUserRole_EmitsNothing: when the vocab role
// map yields no credentialed mission-user role (here: default role without
// a CredentialRef), the user-scoped steps would all run unauthenticated —
// the generator must emit nothing rather than guess a role.
func TestMissionSeedCases_NoMissionUserRole_EmitsNothing(t *testing.T) {
	// The vocab maps the user-scoped routes to web, but the web role carries
	// no CredentialRef (missionSendFixture's shape).
	svc := missionSendFixture()
	svc.Vocabulary.HTTPRoleRoutes = []project.VocabRoleRoute{{Prefix: "/api/admin", Role: "admin"}}
	svc.Vocabulary.HTTPDefaultRole = "web"
	if got := missionSeedCases(svc, map[string]bool{"bridge": true}); got != nil {
		t.Fatalf("emitted %d cases without a credentialed mission-user role", len(got))
	}
}

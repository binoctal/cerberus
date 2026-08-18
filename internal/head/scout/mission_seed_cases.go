package scout

import (
	"fmt"
	"os"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// missionSeedCases seeds ONE real mission and observes the orchestration
// pushes on a web connection. Setup unlocks the whole gating chain (spec
// §1-§6): plan feature gate → user plan → planner provider → agent row
// (stall guard) → mission create. Emitted only when the service declares
// workflow-family vocab edges and the bridge role is a real process.
//
// The mission user must be the WEB actor's user (the dev backdoor user the
// bridges pair under), NOT the admin: open-agents scopes device ownership and
// room broadcast per user — checkDeviceOnline (workflows.ts:371-379) requires
// devices.user_id = mission user, and workflow:* pushes go to the mission
// user's room. Under the admin JWT the orchestrator logs "no online devices"
// forever (verified live 2026-08-18) and no frame ever reaches the web
// connection. So the user-scoped steps (auth/me, agents, missions) run under
// the web role's JWT, while admin routes (plan seed, user switch, provider)
// stay under the admin JWT. The web actor carries no statically-known user id,
// so step 0 captures it from GET /api/auth/me (top-level id, auth.ts:601-626)
// and the user-plan step consumes it as {{case.userId}}.
func missionSeedCases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if !realRoles["bridge"] || svc.Protocol == nil || svc.Vocabulary == nil || !hasWorkflowEdges(svc.Vocabulary) {
		return nil
	}
	admin := ""
	if r := svc.Protocol.Roles["admin"]; r != nil && r.CredentialRef != "" {
		admin = "admin"
	}
	host := serviceHost(svc.URL)
	plannerKey := os.Getenv("CERBERUS_PLANNER_API_KEY")
	plannerURL := os.Getenv("CERBERUS_PLANNER_API_URL")
	plannerModel := os.Getenv("CERBERUS_PLANNER_MODEL")
	steps := []agent.TestStep{
		// 0. Capture the web user's id (the future mission user — see the
		// function comment: it must own the bridge devices and the room).
		{Action: "http_request", URL: host + "/api/auth/me", Method: "GET",
			AuthRole: "web", ExpectStatusClass: "2xx",
			Capture: map[string]string{"id": "userId"}},
		// 1. Seed the plan. NEVER max_concurrent_tasks (spec §2 0-trap).
		// api_hourly/api_daily must be lifted too: plan-limits.ts deep-merges
		// unset keys with the free-plan fallback (api_hourly 100, api_daily
		// 500), and the route sweep's ~130 admin writes under the same JWT
		// exhaust api_hourly within the hour — the mission-setup POSTs then
		// 429 (HOURLY_RATE_LIMIT_EXCEEDED) before any orchestration starts.
		{Action: "http_request", URL: host + "/api/admin/billing/plans", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body:    `{"name":"cerberus-dogfood","price_monthly":0,"limits":{"feature_gates":{"workflows":true},"rate_limits":{"daily_missions":9999,"api_hourly":9999,"api_daily":9999},"resources":{"max_agents":100}}}`,
			Capture: map[string]string{"id": "planId"}},
		// 2. Switch the user to it (both ids read back in steps 0-1).
		{Action: "http_request", URL: host + "/api/admin/users/{{case.userId}}", Method: "PUT",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: `{"plan":"{{case.planId}}"}`},
	}
	if plannerKey != "" {
		steps = append(steps, agent.TestStep{
			// 3. Planner provider (encrypt-at-rest requires PROVIDER_KEY_KEK in .dev.vars — harness concern, Task 5).
			Action: "http_request", URL: host + "/api/admin/ai-providers", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: fmt.Sprintf(`{"name":"cerberus-planner","provider":"anthropic","api_url":%q,"api_key":%q,"models":[{"id":%q,"display_name":"planner","input_price_per_million":0,"output_price_per_million":0}],"is_active":true}`,
				plannerURL, plannerKey, plannerModel),
		})
	}
	steps = append(steps,
		// 4. Agent row — the stall guard (spec §5): user-scoped route, so it
		// runs under the web (mission) user. The row NAME must equal its
		// baseCli: the planner injects the user's agents as "- baseCli: name"
		// and glm-4.5 sometimes copies that literal into assigned_agent
		// (live-observed 2026-08-18: "claude: cerberus-bridge-agent" → no
		// device with that "CLI" → task skipped forever). With name ==
		// "claude", BOTH of the orchestrator's resolution paths succeed:
		// resolveCliForAgent matches the agents-table name → base_cli, and a
		// raw "claude" falls through to the device's cliEnabled map (the
		// dogfood shim puts a claude binary on the bridge PATH).
		agent.TestStep{Action: "http_request", URL: host + "/api/agents", Method: "POST",
			AuthRole: "web", ExpectStatusClass: "2xx",
			Body:    `{"name":"claude","baseCli":"claude"}`,
			Capture: map[string]string{"id": "agentId"}},
		// 5. The mission itself (create returns {mission:{id}} → dot-path
		// capture). Web role: the mission user must be the device owner.
		agent.TestStep{Action: "http_request", URL: host + "/api/missions", Method: "POST",
			AuthRole: "web", ExpectStatusClass: "2xx",
			Body:    `{"inputText":"Reply with the single word done. Do not create files.","deviceIds":["{{bridge.deviceId}}"],"autoConfirm":true}`,
			Capture: map[string]string{"mission.id": "missionId"}},
		// 6. Observe on a web connection: the pushes the orchestration path
		// REALLY emits (live-verified 2026-08-18). startTaskSession pushes
		// workflow:task_progress (step:"started") when the task session comes
		// up — NOT task_started (that type only exists as the echo of
		// web-origin start/start_task, covered by the send cases) — and the
		// PTY echo of the task prompt (which carries the [QUESTION]
		// instruction) deterministically yields workflow:task_question.
		// task_result / completion are NOT expected: the bridge reports
		// completion only via its HTTP callback, whose URL is built from the
		// ws:// server URL (unsupported protocol scheme — live-verified), so
		// the orchestrator never learns the task finished.
		agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 300},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_question", Timeout: 120},
	)
	return []agent.TestCase{{
		ID: wsCaseID(svc.Name, "wf", "mission-seed"), Service: svc.Name, Target: svc.URL,
		Name:   "seeded mission drives real orchestration end to end",
		Action: "ws_flow", Priority: 0.8,
		Expectation: "mission created (plan gate, provider, agent row seeded), real planner decomposes it, the orchestrator dispatches to the real bridge, and the task session pushes workflow:task_progress + workflow:task_question to the web connection",
		Steps:       steps,
	}}
}

// hasWorkflowEdges: any vocab edge whose type carries the workflow: prefix.
func hasWorkflowEdges(v *project.Vocabulary) bool {
	for _, e := range v.Edges {
		if strings.HasPrefix(e.Type, "workflow:") && !e.Partial && !e.Unsupported {
			return true
		}
	}
	return false
}

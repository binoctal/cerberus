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
// The admin actor carries no statically-known user id (dogfood project.yaml
// declares none), so step 0 captures it from GET /api/auth/me — the route
// returns the JWT user's id at the top level (auth.ts:601-626) — and the
// user-plan step consumes it as {{case.userId}}. All authenticated steps run
// under the admin JWT (AuthRole injection), so the captured id IS the mission
// user: the plan switch, agent row, and mission all land on that user.
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
		// 0. Capture the admin JWT's user id (the future mission user).
		{Action: "http_request", URL: host + "/api/auth/me", Method: "GET",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Capture: map[string]string{"id": "userId"}},
		// 1. Seed the plan. NEVER max_concurrent_tasks (spec §2 0-trap).
		{Action: "http_request", URL: host + "/api/admin/billing/plans", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body:    `{"name":"cerberus-dogfood","price_monthly":0,"limits":{"feature_gates":{"workflows":true},"rate_limits":{"daily_missions":9999}}}`,
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
		// 4. Agent row — the stall guard (spec §5): user-scoped route.
		// baseCli "claude" is one of the bridge PTY's cliEnabled capabilities
		// (bridge config.example.json; cli_detect.go enables detected CLIs,
		// and the dogfood shim puts a claude binary on PATH).
		agent.TestStep{Action: "http_request", URL: host + "/api/agents", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body:    `{"name":"cerberus-bridge-agent","baseCli":"claude"}`,
			Capture: map[string]string{"id": "agentId"}},
		// 5. The mission itself (create returns {mission:{id}} → dot-path capture).
		agent.TestStep{Action: "http_request", URL: host + "/api/missions", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body:    `{"inputText":"Reply with the single word done. Do not create files.","deviceIds":["{{bridge.deviceId}}"],"autoConfirm":true}`,
			Capture: map[string]string{"mission.id": "missionId"}},
		// 6. Observe on a web connection: deterministic pushes only.
		agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_started", Timeout: 600},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 600},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_result", Timeout: 600},
		// Completion signal is job_status (out-of-band type; job_completed is dead — spec §9).
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:job_status", Timeout: 600},
	)
	return []agent.TestCase{{
		ID: wsCaseID(svc.Name, "wf", "mission-seed"), Service: svc.Name, Target: svc.URL,
		Name:   "seeded mission drives the workflow family end to end",
		Action: "ws_flow", Priority: 0.8,
		Expectation: "mission created (plan gate, provider, agent row seeded) and the real bridge executes it: task_started, task_progress, task_result observed on web; completion via workflow:job_status",
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

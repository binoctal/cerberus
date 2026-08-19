package scout

import (
	"fmt"
	"os"
	"strings"
	"time"

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
		// Negative priority so each run's row WINS the resolveAiConfig tie
		// (orders by priority ASC, created_at ASC; every dogfood run POSTs a
		// fresh row, and with the default priority 0 the OLDEST row would
		// win forever — verified live 2026-08-18: 7 piled-up glm-4.5 rows,
		// the 2026-08-17 one silently pinning the planner model).
		providerPriority := -time.Now().Unix()
		steps = append(steps, agent.TestStep{
			// 3. Planner provider (encrypt-at-rest requires PROVIDER_KEY_KEK in .dev.vars — harness concern, Task 5).
			Action: "http_request", URL: host + "/api/admin/ai-providers", Method: "POST",
			AuthRole: admin, ExpectStatusClass: "2xx",
			Body: fmt.Sprintf(`{"name":"cerberus-planner","provider":"anthropic","api_url":%q,"api_key":%q,"models":[{"id":%q,"display_name":"planner","input_price_per_million":0,"output_price_per_million":0}],"is_active":true,"priority":%d}`,
				plannerURL, plannerKey, plannerModel, providerPriority),
		})
	}
	steps = append(steps,
		// 4. Agent row — the stall guard (spec §5): user-scoped route, so it
		// runs under the web (mission) user. The row NAME must equal its
		// baseCli: the planner injects the user's agents as "- baseCli: name"
		// and glm-4.5 sometimes copies that literal into assigned_agent
		// (live-observed 2026-08-18: "claude: cerberus-bridge-agent" → no
		// device with that "CLI" → task skipped forever). name == baseCli ==
		// "claude-pty" makes every glm pick safe (it copies baseCli or name,
		// both resolve). The dispatch message carries assigned_agent
		// verbatim and the bridge picks the CLI by it: "claude" would run
		// the ACP adapter (npx @agentclientprotocol/claude-agent-acp),
		// which in the offline dogfood env never finishes (60s connect
		// timeout, then a JSON-protocol process fed raw prompt text that
		// errors on every line — live-verified 2026-08-19), while
		// "claude-pty" runs the claude binary on PATH (the dogfood shim):
		// it echoes the prompt and, on a task prompt, exits after the
		// [QUESTION] line — the session exit that fires the completion
		// callback. Legacy rows with base_cli "claude" from pre-2026-08-19
		// runs must be cleaned from the dev D1 once (see the env doc), or
		// glm may pick them instead.
		agent.TestStep{Action: "http_request", URL: host + "/api/agents", Method: "POST",
			AuthRole: "web", ExpectStatusClass: "2xx",
			Body:    `{"name":"claude-pty","baseCli":"claude-pty"}`,
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
		agent.TestStep{Action: "ws_connect", ConnectionID: "web", Role: "web"},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_progress", Timeout: 300},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_question", Timeout: 120},
		// 7. Completion frames (reopened 2026-08-19 after the open-agents
		// callback fix, fix/workflow-callback-url): the bridge now reports
		// completion via its HTTP callback (POST /api/missions/internal/
		// orchestrator/event with X-Internal-Secret), so the orchestrator's
		// handleTaskResult broadcasts workflow:task_completed and, once every
		// task is done, finalizeMissionIfDone broadcasts workflow:job_status
		// (status:"completed"). Before the fix the callback POST died on
		// "unsupported protocol scheme" (ws:// APIURL) and missions could
		// only ever leave via stuckRecovery.
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_completed", Timeout: 600},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:job_status", Timeout: 120},
		// 8. Merge the task branch: web-origin workflow:task_merge (room-
		// routed to the bridge via payload.deviceId since the same open-agents
		// fix added task_merge to the web→bridge whitelist and the DO
		// /broadcast routing) makes the bridge merge the task worktree branch
		// and reply workflow:task_result (merged:true) — the ONLY emitter of
		// the bridge→web task_result frame. taskId is deterministic,
		// {missionId}_t{i} (missions.ts task-row build), and t0 exists for any
		// plan the planner produces; the no-file-change task leaves the branch
		// at HEAD so the merge is a clean "already up to date".
		agent.TestStep{Action: "ws_send", ConnectionID: "web",
			Message: wsSendBodyAny("workflow:task_merge", map[string]any{
				"deviceId": "{{bridge.deviceId}}",
				"jobId":    "{{case.missionId}}",
				"taskId":   "{{case.missionId}}_t0",
			})},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_result", Timeout: 60},
		// 9. Failure path (reopened 2026-08-19): a second mission whose tasks
		// must fail. The planner folds the inputText into the task
		// title/description; the dogfood shim exits 1 on a CERBERUS_FAIL line,
		// so the bridge reports task_error via its (now working) callback and
		// the orchestrator retries (MAX_RETRIES 3, backoff 5s/15s/45s). The
		// retry fallback ladder climbs claude-pty → claude (npx ACP) → codex;
		// every rung except claude-pty is a fail-fast exit-1 stub in the
		// dogfood shim dir (a REAL fallback CLI would stay alive and the 4th
		// error would never arrive — live-verified: tasks stuck at
		// retry_count=3 on the host's real codex). Retry exhaustion
		// broadcasts workflow:task_failed — the only emitter of that frame.
		agent.TestStep{Action: "http_request", URL: host + "/api/missions", Method: "POST",
			AuthRole: "web", ExpectStatusClass: "2xx",
			Body:    `{"inputText":"CERBERUS_FAIL: this mission must fail. Every task prints the marker CERBERUS_FAIL in its description and then exits with an error. Do not create files.","deviceIds":["{{bridge.deviceId}}"],"autoConfirm":true}`,
			Capture: map[string]string{"mission.id": "failMissionId"}},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_failed", Timeout: 600},
		// 10. Merge-failure task_error: the ONLY emitter of the bridge→web
		// workflow:task_error frame is a failed branch merge
		// (handleWorkflowTaskMerge). A taskId with no worktree branch makes
		// `git merge task-...` fail ("not something we can merge"), which
		// reports errorType merge_failed — deterministic, no bridge breakage.
		agent.TestStep{Action: "ws_send", ConnectionID: "web",
			Message: wsSendBodyAny("workflow:task_merge", map[string]any{
				"deviceId": "{{bridge.deviceId}}",
				"jobId":    "{{case.failMissionId}}",
				"taskId":   "{{case.failMissionId}}_no-branch",
			})},
		agent.TestStep{Action: "ws_receive", ConnectionID: "web", Type: "workflow:task_error", Timeout: 60},
	)
	return []agent.TestCase{{
		ID: wsCaseID(svc.Name, "wf", "mission-seed"), Service: svc.Name, Target: svc.URL,
		Name:   "seeded mission drives real orchestration end to end",
		Action: "ws_flow", Priority: 0.8,
		Expectation: "mission created (plan gate, provider, agent row seeded), real planner decomposes it, the orchestrator dispatches to the real bridge, the task session pushes workflow:task_progress + workflow:task_question, completion flows back through the bridge HTTP callback (workflow:task_completed + workflow:job_status), a web-initiated task_merge elicits workflow:task_result, a failing mission exhausts its retries into workflow:task_failed, and a branchless merge elicits workflow:task_error",
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

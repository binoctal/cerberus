package scout

import (
	"fmt"
	"maps"
	"slices"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// sortedRoleNames: deterministic role iteration order.
func sortedRoleNames(roles map[string]*project.ProtocolRole) []string {
	return slices.Sorted(maps.Keys(roles))
}

// acpE2ECases emits, for every real-process protocol role that declares
// acp_cli, ONE deterministic ACP-path case — and, when the role also sets
// acp_real, ONE real-LLM case. Both connect as the emulated client role and
// route a session at the REAL device by cross-actor deviceId placeholder,
// but with the ACP cliType (the bridge resolves e.g. "claude" to
// npx @agentclientprotocol/claude-agent-acp, its PREFERRED adapter — PTY is
// only the fallback). The deterministic layer runs against a fake ACP agent
// (dogfood shim/npx); the real layer runs against the real agent (a real
// CLI + real LLM — dogfood's only true agent-LLM execution). SUT fact
// lives in the protocol role declaration, not here.
func acpE2ECases(svc project.Service, realRoles map[string]bool) []agent.TestCase {
	if svc.Protocol == nil || len(realRoles) == 0 {
		return nil
	}
	client := ""
	for _, roleName := range sortedRoleNames(svc.Protocol.Roles) {
		if realRoles[roleName] {
			continue
		}
		if r := svc.Protocol.Roles[roleName]; r != nil && r.CredentialRef != "" && !r.HTTPOnly {
			client = roleName
			break
		}
	}
	if client == "" {
		return nil
	}
	var cases []agent.TestCase
	for _, roleName := range sortedRoleNames(svc.Protocol.Roles) {
		if !realRoles[roleName] {
			continue
		}
		role := svc.Protocol.Roles[roleName]
		if role == nil || role.ACPCli == "" {
			continue
		}
		cases = append(cases, acpOneCase(svc, client, roleName, role, false))
		if role.ACPReal {
			cases = append(cases, acpOneCase(svc, client, roleName, role, true))
		}
	}
	return cases
}

// acpOneCase builds one ACP-path session case. real=false is the
// deterministic layer: the prompt requests an exact marker the fake ACP
// agent replies with, and receive windows are tight. real=true targets a
// real CLI + LLM: the prompt asks for a short human-readable confirmation
// and every receive window is 120s (real agent latency, first-token delays
// included).
func acpOneCase(svc project.Service, client, roleName string, role *project.ProtocolRole, real bool) agent.TestCase {
	sessionID := "e2e-acp-" + svc.Name + "-" + roleName
	if real {
		sessionID += "-real"
	}
	startPayload := map[string]string{
		"sessionId": sessionID,
		"cliType":   role.ACPCli,
		"workDir":   "/tmp",
		"cols":      "80",
		"rows":      "24",
		"deviceId":  "{{" + roleName + ".deviceId}}",
	}
	for k, v := range role.RequestPayload["session:start"] {
		startPayload[k] = v
	}
	prompt, timeout := "Reply with exactly: CERBERUS_ACP_OK", 30
	suffix, expectation := "-acpe2e", fmt.Sprintf(
		"%s: %s starts an ACP-protocol session on the REAL %s process via deviceId routing, the fake ACP agent replies with the marker text CERBERUS_ACP_OK over the adapter, and the session stops",
		svc.Name, client, roleName)
	if real {
		prompt, timeout = "Reply with one short sentence confirming you are a real AI agent. Do not create files.", 120
		suffix = "-acpreal"
		expectation = fmt.Sprintf(
			"%s: %s starts an ACP-protocol session on the REAL %s process via deviceId routing, a REAL AI agent (real CLI, real LLM) replies with a short substantive human-readable confirmation — not an error, not empty — and the session stops",
			svc.Name, client, roleName)
	}
	return agent.TestCase{
		ID:          wsCaseID(svc.Name, roleName+suffix, "session"),
		Name:        fmt.Sprintf("%s %s routes an ACP session at real %s", svc.Name, client, roleName),
		Service:     svc.Name,
		Target:      svc.URL,
		Action:      "ws_flow",
		Expectation: expectation,
		Priority:    0.8,
		Claims:      roleClaimBindings(role),
		Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: client, Role: client},
			{Action: "ws_send", ConnectionID: client, Message: wsSendBody("session:start", startPayload)},
			{Action: "ws_receive", ConnectionID: client, Type: "session:started", Timeout: timeout},
			{Action: "ws_send", ConnectionID: client, Message: wsSendBody("session:send", map[string]string{
				"sessionId": sessionID, "content": prompt, "deviceId": startPayload["deviceId"],
			})},
			{Action: "ws_receive", ConnectionID: client, Type: "chat:response", Aliases: []string{"session:output-batch"}, Timeout: timeout},
			{Action: "ws_send", ConnectionID: client, Message: wsSendBody("session:stop", map[string]string{
				"sessionId": sessionID, "deviceId": startPayload["deviceId"],
			})},
			{Action: "ws_receive", ConnectionID: client, Type: "session:stopped", Timeout: timeout},
		},
	}
}

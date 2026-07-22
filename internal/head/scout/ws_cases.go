package scout

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// WSCases generates deterministic WS test cases from a project's declared
// protocols: for each role on each WS service, one ws_connect setup case plus
// one decisive ws_receive case per verification-point type (the role's
// handshake await_type, plus any routing type named in the goal). Returns nil
// when no service declares a protocol. The agent Steer LLM orchestrates the
// actual connect/send/receive; these cases seed the plan with WS intent.
//
// Connection-isolation note: WebSocket connections are namespaced per case
// (<caseID>:<connectionID>, see WebSocketExecutor.caseNamespace), so the
// ws_connect setup case's connection is NOT shared with the dependent receive
// cases — each case connects independently within its own Steer loop. The
// DependsOn link is therefore ordering-only (the connect case runs first), not
// a connection-sharing dependency. Sharing one connection across a
// connect->send->receive sequence requires a single multi-step case (the
// deferred TestCase.Steps path; see the M3-2 design spec Open Questions).
func WSCases(cfg *project.Config, goal string) []agent.TestCase {
	if cfg == nil {
		return nil
	}
	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		if svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
			continue
		}
		// Iterate roles in sorted name order so the returned slice is
		// deterministic across runs regardless of map iteration order.
		for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
			role := svc.Protocol.Roles[roleName]
			connectID := wsCaseID(svc.Name, roleName, "connect")
			cases = append(cases, agent.TestCase{
				ID:          connectID,
				Name:        fmt.Sprintf("%s %s connects", svc.Name, roleName),
				Service:     svc.Name,
				Target:      svc.URL,
				Action:      "ws_connect",
				Background:  true,
				Body:        wsBody(roleName, ""),
				Expectation: fmt.Sprintf("%s role %s establishes the connection", svc.Name, roleName),
				Priority:    0.5,
			})
			for _, typ := range wsDecisiveTypes(role, goal) {
				cases = append(cases, agent.TestCase{
					ID:          wsCaseID(svc.Name, roleName, typ),
					Name:        fmt.Sprintf("%s %s receives %s", svc.Name, roleName, typ),
					Service:     svc.Name,
					Target:      svc.URL,
					Action:      "ws_receive",
					Body:        wsBody(roleName, typ),
					Expectation: fmt.Sprintf("%s role %s receives a %s message", svc.Name, roleName, typ),
					DependsOn:   agent.Deps{connectID},
					Priority:    0.8,
				})
			}
		}
	}
	return cases
}

// wsDecisiveTypes returns the routing types to assert on for a role: the
// handshake await_type (if any) plus any type literally named in the goal that
// is not already included. Deterministic; no LLM.
func wsDecisiveTypes(role *project.ProtocolRole, goal string) []string {
	var types []string
	if role != nil && role.Handshake != nil && role.Handshake.AwaitType != "" {
		types = append(types, role.Handshake.AwaitType)
	}
	for _, t := range wsTypesNamedInGoal(goal) {
		if !contains(types, t) {
			types = append(types, t)
		}
	}
	return types
}

// wsSendVerbs are goal verbs that mark the following colon token as something
// the CLIENT sends (not a receive target). A token whose immediately preceding
// word is one of these is excluded from ws_receive generation. Provisional —
// tune via dogfooding.
var wsSendVerbs = map[string]bool{
	"send": true, "sends": true, "sending": true,
	"emit": true, "emits": true,
	"publish": true, "publishes": true,
}

// wsTypesNamedInGoal finds candidate routing-type tokens in the goal text. A
// simple heuristic: colon-bearing tokens (e.g. "permission:response") are
// common WS routing keys. A token immediately preceded by a send-verb (see
// wsSendVerbs) is client-sent and excluded; tokens without such context default
// to receive (included). Deterministic; no LLM. Provisional — tune via dogfooding.
func wsTypesNamedInGoal(goal string) []string {
	var out []string
	fields := strings.Fields(goal)
	for i, field := range fields {
		// Strip punctuation incl. braces so a goal template like
		// "{type: device:command}" yields "device:command", not
		// "device:command}" or "{type:".
		f := strings.Trim(field, ".,;:\"'(){}")
		if f == "type:" {
			continue // the default routing-key field name, not a type value
		}
		if !strings.Contains(f, ":") {
			continue
		}
		if contains(out, f) {
			continue
		}
		// Direction: a token immediately preceded by a send-verb is something
		// the CLIENT sends, so it is not a receive target — skip it. Tokens
		// without a send-verb context default to receive (existing behavior).
		if i > 0 && wsSendVerbs[strings.ToLower(strings.Trim(fields[i-1], ".,;:\"'(){}"))] {
			continue
		}
		out = append(out, f)
	}
	return out
}

func wsBody(role, typ string) string {
	m := map[string]string{"role": role}
	if typ != "" {
		m["type"] = typ
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func wsCaseID(service, role, typ string) string {
	return "ws-" + service + "-" + role + "-" + sanitizeTypeID(typ)
}

// sanitizeTypeID turns a routing type into an ID-safe token.
func sanitizeTypeID(typ string) string {
	r := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return r.Replace(typ)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

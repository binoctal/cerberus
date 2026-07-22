package scout

import (
	"encoding/json"
	"fmt"
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
func WSCases(cfg *project.Config, goal string) []agent.TestCase {
	if cfg == nil {
		return nil
	}
	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		if svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
			continue
		}
		for roleName, role := range svc.Protocol.Roles {
			connectID := wsCaseID(svc.Name, roleName, "connect")
			cases = append(cases, agent.TestCase{
				ID:          connectID,
				Name:        fmt.Sprintf("%s %s connects", svc.Name, roleName),
				Service:     svc.Name,
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

// wsTypesNamedInGoal finds candidate routing-type tokens in the goal text. A
// simple heuristic: colon-bearing tokens (e.g. "permission:response") are
// common WS routing keys. Provisional — tune via dogfooding.
func wsTypesNamedInGoal(goal string) []string {
	var out []string
	for _, field := range strings.Fields(goal) {
		f := strings.Trim(field, ".,;:\"'()")
		if strings.Contains(f, ":") && !contains(out, f) {
			out = append(out, f)
		}
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

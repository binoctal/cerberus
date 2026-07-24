package scout

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// relayIntent is the LLM-authored relay description carried in a ws_relay case's
// body. Roles is the ordered set of connections to open (the peer-join signal
// receiver first, so the DO pushes to an already-connected client); Steps is the
// ordered send/receive sequence across them. Assert (receive only) is a
// dotted-path -> value map passed through to the step.
type relayIntent struct {
	Roles []string    `json:"roles"`
	Steps []relayStep `json:"steps"`
}

type relayStep struct {
	Do      string         `json:"do"`      // "send" | "receive"
	Role    string         `json:"role"`    // a connection named in Roles
	Type    string         `json:"type"`    // message routing type
	Aliases []string       `json:"aliases"` // receive only: additional matching types
	Assert  map[string]any `json:"assert"`
}

// expandWSRelayCases expands every ws_relay case in plan.Cases into a
// deterministic multi-connection Steps case (connect each role in Roles order,
// then the ordered send/receive) and replaces it in place. It returns the roles
// covered per service so WSCases can skip redundant connect cases for them.
// Invalid intents (unknown service/protocol/role, fewer than 2 roles, malformed
// body, bad step) are dropped — the case is removed; the run never fails. Pure:
// no LLM, no I/O, deterministic.
func expandWSRelayCases(cfg *project.Config, plan *agent.TestPlan) map[string]map[string]bool {
	covered := map[string]map[string]bool{}
	if cfg == nil || plan == nil {
		return covered
	}
	svcByName := map[string]*project.Service{}
	for i := range cfg.Services {
		svcByName[cfg.Services[i].Name] = &cfg.Services[i]
	}
	out := make([]agent.TestCase, 0, len(plan.Cases))
	for _, c := range plan.Cases {
		if c.Action != "ws_relay" {
			out = append(out, c)
			continue
		}
		exp, ok := expandOneRelayCase(svcByName, c)
		if !ok {
			continue // dropped: malformed/unresolvable ws_relay intent
		}
		out = append(out, exp.tc)
		if covered[exp.service] == nil {
			covered[exp.service] = map[string]bool{}
		}
		for _, r := range exp.roles {
			covered[exp.service][r] = true
		}
	}
	plan.Cases = out
	return covered
}

type expandedRelay struct {
	tc      agent.TestCase
	service string
	roles   []string
}

// expandOneRelayCase resolves the case's service + protocol, validates the
// intent, and assembles the Steps case. ok=false when the case should be dropped.
func expandOneRelayCase(svcByName map[string]*project.Service, c agent.TestCase) (expandedRelay, bool) {
	svc := svcByName[c.Service]
	if svc == nil || svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
		return expandedRelay{}, false
	}
	var intent relayIntent
	if err := json.Unmarshal([]byte(c.Body), &intent); err != nil {
		return expandedRelay{}, false
	}
	if len(intent.Roles) < 2 {
		return expandedRelay{}, false
	}
	if len(intent.Steps) == 0 {
		return expandedRelay{}, false // a relay must have at least one send/receive
	}
	// Every named role must be declared by this ONE protocol; collect the set so
	// step roles can be checked against it.
	declared := map[string]*project.ProtocolRole{}
	for _, r := range intent.Roles {
		if _, dup := declared[r]; dup {
			return expandedRelay{}, false // duplicate role; roles must be distinct
		}
		role := svc.Protocol.Roles[r]
		if role == nil {
			return expandedRelay{}, false
		}
		declared[r] = role
	}
	for _, st := range intent.Steps {
		if (st.Do != "send" && st.Do != "receive") || st.Type == "" || declared[st.Role] == nil {
			return expandedRelay{}, false
		}
	}
	// Assemble: one connect per role (intent order), then the ordered steps.
	steps := make([]agent.TestStep, 0, len(intent.Roles)+len(intent.Steps))
	for _, r := range intent.Roles {
		steps = append(steps, agent.TestStep{Action: "ws_connect", ConnectionID: r, Role: r})
	}
	for _, st := range intent.Steps {
		switch st.Do {
		case "send":
			steps = append(steps, agent.TestStep{Action: "ws_send", ConnectionID: st.Role, Message: wsSendBody(st.Type)})
		case "receive":
			steps = append(steps, agent.TestStep{
				Action: "ws_receive", ConnectionID: st.Role, Type: st.Type,
				Aliases: st.Aliases, Asserts: st.Assert, Timeout: relayRecvTimeout(declared[st.Role]),
			})
		}
	}
	// Deterministic ID: sorted roles (connect order stays the intent's order).
	sortedRoles := append([]string(nil), intent.Roles...)
	slices.Sort(sortedRoles)
	return expandedRelay{
		tc: agent.TestCase{
			ID:          "ws-" + c.Service + "-relay-" + strings.Join(sortedRoles, "-"),
			Name:        c.Name,
			Service:     c.Service,
			Target:      svc.URL,
			Action:      "ws_flow", // informational; runSteps routes on len(Steps) > 0
			Expectation: c.Expectation,
			Steps:       steps,
		},
		service: c.Service,
		roles:   intent.Roles,
	}, true
}

// relayRecvTimeout returns the receive-await budget for a step on role r: the
// role's declared handshake timeout (seconds) if any, else 0 (executor default).
func relayRecvTimeout(r *project.ProtocolRole) int {
	if r != nil && r.Handshake != nil && r.Handshake.Timeout > 0 {
		return r.Handshake.Timeout
	}
	return 0
}

package scout

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

// WSCases generates WS test cases for cfg/goal with no roles covered. It is
// kept for compatibility (existing callers/tests); new code calls WSCasesCovered.
func WSCases(cfg *project.Config, goal string) []agent.TestCase {
	return WSCasesCovered(cfg, goal, nil)
}

// WSCasesCovered generates deterministic WS test cases from a project's declared
// protocols. Per role on each WS service it emits ONE ws_flow Steps case:
//
//   - connect → send → receive (sharing one connection_id) when the goal pairs a
//     client-sent type (send-verb-introduced) with a following receive type; OR
//   - connect + one receive per decisive type when no exchange is detected
//     (receive-only, handshake-only, or unrelated goals).
//
// Both forms run through runSteps (deterministic, no Steer). The WS connection
// table is keyed by <caseID>:<connectionID>, so a role's connect and receives
// must live in ONE case to share a socket — folding them into Steps (rather than
// separate connect + DependsOn receive cases, whose per-case namespaces could
// never share a connection) is what makes the no-exchange path runnable. The
// connect step carries Role, so the executor auto-awaits the role's
// handshake.await_type; receive steps therefore exclude the handshake await_type
// (consumed by connect) and any relay signal. A role with no decisive
// non-handshake type yields a connect-only Steps case. Returns nil when no
// service declares a protocol.
//
// Determinism: roles are iterated in sorted name order; the exchange detector
// picks the first send/receive pair; Asserts are parsed in goal order.
func WSCasesCovered(cfg *project.Config, goal string, covered map[string]map[string]bool) []agent.TestCase {
	if cfg == nil {
		return nil
	}
	var cases []agent.TestCase
	for _, svc := range cfg.Services {
		if svc.Protocol == nil || len(svc.Protocol.Roles) == 0 {
			continue
		}
		// Deterministic peer-join relay cases: a role with an OPTIONAL handshake
		// receives its signal when a peer connects. These are protocol-derivable
		// (no LLM). A peer-join signal times out on ANY lone connection, so the
		// redundant single-conn receive of a covered type is suppressed for every
		// role below (not just the receiver). Coexist with LLM ws_relay (A1): when
		// a receiver role is already covered by an LLM relay case, the
		// deterministic relay is redundant and is dropped.
		relayCases, relayCovered, _ := wsRelayCases(svc)
		svcCovered := covered[svc.Name]
		for _, rc := range relayCases {
			// The receiver role is the first step's Role (A-first connect order).
			if !svcCovered[rc.Steps[0].Role] {
				cases = append(cases, rc)
			}
		}
		relaySignals := map[string]bool{}
		for receiver, types := range relayCovered {
			if svcCovered[receiver] {
				continue
			}
			for typ := range types {
				relaySignals[typ] = true
			}
		}
		// Finding-2: only EMITTED relay cases open sockets. A relay case is
		// dropped above when its receiver is LLM-covered (A1 coexistence), so the
		// peers of a dropped case are NOT connected by any relay and must still
		// emit their own connect. Build relayConnected from emitted cases only
		// (deterministic — iterates the sorted relayCases slice). Mirrors the
		// relayCovered → relaySignals filter directly above.
		relayConnected := map[string]bool{}
		for _, rc := range relayCases {
			if svcCovered[rc.Steps[0].Role] {
				continue
			}
			for _, s := range rc.Steps {
				if s.Action == "ws_connect" {
					relayConnected[s.Role] = true
				}
			}
		}
		// Iterate roles in sorted name order so the returned slice is
		// deterministic across runs regardless of map iteration order.
		for _, roleName := range slices.Sorted(maps.Keys(svc.Protocol.Roles)) {
			if covered[svc.Name][roleName] {
				// Role-level skip: a ws_relay already connects this role, so
				// suppress ALL of WSCases' forms for it (connect, connect+receive,
				// and the ws_flow exchange) to avoid opening redundant sockets.
				continue
			}
			role := svc.Protocol.Roles[roleName]
			if ex, ok := wsExchangeFromGoal(goal); ok {
				cases = append(cases, wsStepsCase(svc, roleName, role, ex))
				continue
			}
			// Finding-2: the deterministic relay case already connects this role
			// (receiver or peer). Its single-conn connect+receive form is
			// redundant (the connect runs in the relay Steps) and, routed through
			// Steer, unreliable. Skip the whole form — a dependent receive would
			// otherwise dangle without its connect. goal-exchange wsStepsCase
			// above is preserved.
			if relayConnected[roleName] {
				continue
			}
			// Finding-2: emit ONE ws_flow Steps case per role. The connect step
			// and each receive share one connection_id, so within this case they
			// share one connection-table namespace (<caseID>:<connID>) and the
			// receives reach the connect's socket. The prior free-form connect
			// case + DependsOn receive cases had DIFFERENT caseIDs, so a receive
			// could never resolve the connect's connection (Background does not
			// broaden the namespace). Routed via runSteps — deterministic, no Steer.
			//
			// The connect step carries Role, so the executor auto-awaits the
			// role's handshake.await_type; the receive steps therefore EXCLUDE
			// the handshake await_type (already consumed by connect — a separate
			// receive would re-await it) and any relay signal (covered by a relay
			// case). A role with no decisive non-handshake type yields a
			// connect-only Steps case (valid: verifies connect + handshake).
			handshakeID := ""
			recvTimeout := 0
			if role != nil && role.Handshake != nil {
				handshakeID = sanitizeTypeID(role.Handshake.AwaitType)
				if role.Handshake.Timeout > 0 {
					// Reuse the handshake timeout as the receive-await budget so
					// a slow server cannot hang the case beyond the role's bound.
					recvTimeout = role.Handshake.Timeout
				}
			}
			steps := []agent.TestStep{{Action: "ws_connect", ConnectionID: roleName, Role: roleName}}
			for _, typ := range wsDecisiveTypes(role, goal) {
				if relaySignals[typ] {
					// The deterministic relay case already receives this
					// peer-join signal across connections; a single-conn
					// receive would time out, so skip it.
					continue
				}
				if sanitizeTypeID(typ) == handshakeID {
					// Awaited and consumed by the connect step's handshake; a
					// separate receive would re-await an already-consumed
					// message. This also collapses a goal-named type that
					// sanitizes to the handshake await_type's ID.
					continue
				}
				steps = append(steps, agent.TestStep{
					Action: "ws_receive", ConnectionID: roleName, Type: typ, Timeout: recvTimeout,
				})
			}
			cases = append(cases, agent.TestCase{
				ID:          wsCaseID(svc.Name, roleName, "connect"),
				Name:        fmt.Sprintf("%s %s connects", svc.Name, roleName),
				Service:     svc.Name,
				Target:      svc.URL,
				Action:      "ws_flow",
				Expectation: fmt.Sprintf("%s role %s establishes the connection", svc.Name, roleName),
				Priority:    0.5,
				Steps:       steps,
			})
		}
	}
	return cases
}

// wsRelayCases emits deterministic multi-connection relay Steps cases for the
// peer-join signals in a service's protocol: each role with an OPTIONAL handshake
// (await_type T) receives T when a peer connects. Returns the cases, the
// (role → set(signalType)) pairs they cover (so the per-role loop can skip the
// redundant single-connection receive), and connectedRoles — every role a relay
// case opens a socket for (receiver + peers), so the per-role loop can skip the
// redundant single-connection connect form. Pure; no LLM. Deterministic (sorted).
func wsRelayCases(svc project.Service) ([]agent.TestCase, map[string]map[string]bool, map[string]bool) {
	var cases []agent.TestCase
	covered := map[string]map[string]bool{}
	connectedRoles := map[string]bool{}
	if svc.Protocol == nil || len(svc.Protocol.Roles) < 2 {
		return cases, covered, connectedRoles
	}
	names := slices.Sorted(maps.Keys(svc.Protocol.Roles))
	for _, aName := range names {
		a := svc.Protocol.Roles[aName]
		if a == nil || a.Handshake == nil || !a.Handshake.Optional || a.Handshake.AwaitType == "" {
			continue
		}
		var peers []string
		for _, p := range names {
			if p != aName {
				peers = append(peers, p)
			}
		}
		signal := a.Handshake.AwaitType
		steps := []agent.TestStep{{Action: "ws_connect", ConnectionID: aName, Role: aName}}
		for _, p := range peers {
			steps = append(steps, agent.TestStep{Action: "ws_connect", ConnectionID: p, Role: p})
		}
		steps = append(steps, agent.TestStep{
			Action: "ws_receive", ConnectionID: aName, Type: signal, Timeout: a.Handshake.Timeout,
		})
		cases = append(cases, agent.TestCase{
			ID:      "ws-" + svc.Name + "-relay-" + aName + "-signal-" + sanitizeTypeID(signal),
			Name:    fmt.Sprintf("%s %s receives relayed %s on peer join", svc.Name, aName, signal),
			Service: svc.Name, Target: svc.URL, Action: "ws_flow",
			Expectation: fmt.Sprintf("%s role %s receives %s relayed when a peer connects", svc.Name, aName, signal),
			Priority:    0.8, Steps: steps,
		})
		if covered[aName] == nil {
			covered[aName] = map[string]bool{}
		}
		covered[aName][signal] = true
		// Finding-2: this relay case opens a socket for the receiver and every
		// peer, so WSCasesCovered can skip their redundant single-conn
		// connect+receive form (the connect runs in this relay case's Steps).
		connectedRoles[aName] = true
		for _, p := range peers {
			connectedRoles[p] = true
		}
	}
	return cases, covered, connectedRoles
}

// wsStepsCase builds a deterministic multi-step WS exchange case for a role:
// connect → send → receive, all sharing one connection_id. The role's handshake
// await_type is auto-awaited by the executor on the ws_connect step (via the
// Role field), so no separate handshake step is emitted. Asserts are derived
// from the goal (path→value map) and may be nil for arrival-only receives.
func wsStepsCase(svc project.Service, role string, r *project.ProtocolRole, ex wsExchange) agent.TestCase {
	// One connection per role; the executor namespaces connection-table keys by
	// <caseID>:<connectionID>, so the same role name is stable within this case
	// and does not collide with other cases' connections.
	connID := role
	timeout := 0
	if r != nil && r.Handshake != nil && r.Handshake.Timeout > 0 {
		// Reuse the handshake timeout as the receive-await budget so a slow
		// server cannot hang the case beyond the role's declared bound.
		timeout = r.Handshake.Timeout
	}
	return agent.TestCase{
		ID:          wsCaseID(svc.Name, role, "flow-"+sanitizeTypeID(ex.sendType)+"-"+sanitizeTypeID(ex.recvType)),
		Name:        fmt.Sprintf("%s %s sends %s and receives %s", svc.Name, role, ex.sendType, ex.recvType),
		Service:     svc.Name,
		Target:      svc.URL,
		Action:      "ws_flow",
		Expectation: fmt.Sprintf("%s role %s sends a %s message and receives a %s reply", svc.Name, role, ex.sendType, ex.recvType),
		Priority:    0.8,
		Steps: []agent.TestStep{
			{Action: "ws_connect", ConnectionID: connID, Role: role},
			{Action: "ws_send", ConnectionID: connID, Message: wsSendBody(ex.sendType)},
			{Action: "ws_receive", ConnectionID: connID, Type: ex.recvType, Asserts: ex.asserts, Timeout: timeout},
		},
	}
}

// wsSendBody builds the JSON payload for a ws_send step: a {"type": "<typ>"}
// envelope matching the standard WS routing-key shape. json.Marshal of a
// one-entry string map cannot fail; the error is intentionally ignored.
func wsSendBody(typ string) string {
	b, _ := json.Marshal(map[string]string{"type": typ})
	return string(b)
}

// wsExchange describes a client send → server receive exchange parsed from a
// goal. asserts may be nil (arrival-only receive).
type wsExchange struct {
	sendType string
	recvType string
	asserts  map[string]any
}

// wsExchangeFromGoal detects the FIRST client-sent → server-receive exchange in
// the goal text, reusing the direction heuristic inversely: a colon token
// immediately preceded by a send-verb (see wsSendVerbs) is the sendType; the
// next non-send-verb colon token is the recvType; trailing key=value tokens
// after the recvType become Asserts (parsed by wsParseAsserts). Returns
// ok=false when the goal has no send-verb-introduced type, or when a send type
// has no following receive type to pair with (the latter bails to the
// connect+receive path so the connect/handshake coverage is preserved).
// Deterministic; no LLM.
func wsExchangeFromGoal(goal string) (wsExchange, bool) {
	fields := strings.Fields(goal)
	// Locate the send type: first colon token preceded by a send-verb.
	sendIdx := -1
	sendType := ""
	for i := 1; i < len(fields); i++ {
		f := strings.Trim(fields[i], ".,;:\"'(){}")
		if f == "type:" || !strings.Contains(f, ":") {
			continue
		}
		if wsSendVerbs[strings.ToLower(strings.Trim(fields[i-1], ".,;:\"'(){}"))] {
			sendIdx = i
			sendType = f
			break
		}
	}
	if sendIdx == -1 {
		return wsExchange{}, false
	}
	// Locate the receive type: the next colon token NOT preceded by a send-verb.
	recvType := ""
	recvIdx := -1
	for i := sendIdx + 1; i < len(fields); i++ {
		f := strings.Trim(fields[i], ".,;:\"'(){}")
		if f == "type:" || !strings.Contains(f, ":") {
			continue
		}
		if wsSendVerbs[strings.ToLower(strings.Trim(fields[i-1], ".,;:\"'(){}"))] {
			continue // another client-sent type, not a receive target
		}
		recvType = f
		recvIdx = i
		break
	}
	if recvIdx == -1 {
		// A send with no paired receive is not a runnable exchange. Bail to
		// the connect+receive path so connect/handshake coverage is preserved.
		return wsExchange{}, false
	}
	return wsExchange{sendType: sendType, recvType: recvType, asserts: wsParseAsserts(fields[recvIdx+1:])}, true
}

// wsParseAsserts parses "key=value" tokens into a path→value map. Values are
// typed (bool, int, float, or string — outer quotes stripped). Keys with no dot
// are prefixed "payload." to match the typical {type, payload} WS envelope; a
// key that already contains a dot is used verbatim. Returns nil when no token
// parses, so the receive is arrival-only. Deterministic.
func wsParseAsserts(tokens []string) map[string]any {
	var out map[string]any
	for _, tok := range tokens {
		f := strings.Trim(tok, ".,;:\"'(){}")
		eq := strings.Index(f, "=")
		if eq <= 0 || eq == len(f)-1 {
			continue
		}
		k := f[:eq]
		v := f[eq+1:]
		if k == "" || v == "" {
			continue
		}
		if !strings.Contains(k, ".") {
			k = "payload." + k
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[k] = wsParseAssertValue(v)
	}
	return out
}

// wsParseAssertValue converts a goal assert literal to its typed value: bool
// for true/false, int for integer numerals, float for decimals, otherwise the
// string with outer quotes stripped.
func wsParseAssertValue(s string) any {
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return strings.Trim(s, "\"'")
}

// wsDecisiveTypes returns the routing types to assert on for a role: the
// handshake await_type (if any) plus any type literally named in the goal that
// is not already included. Deterministic; no LLM.
//
// Types are deduped by their sanitized case-ID form: two routing types that
// collapse to the same ID (e.g. "devices-sync" and "devices:sync" both -> the
// ID segment "devices-sync") must not both become receive cases, or they would
// share one case ID and corrupt the DependsOn graph. The first-seen spelling
// wins (handshake await_type is added before goal-named types).
func wsDecisiveTypes(role *project.ProtocolRole, goal string) []string {
	var types []string
	seen := make(map[string]bool)
	add := func(t string) {
		if t == "" {
			return
		}
		id := sanitizeTypeID(t)
		if seen[id] {
			return
		}
		seen[id] = true
		types = append(types, t)
	}
	if role != nil && role.Handshake != nil {
		add(role.Handshake.AwaitType)
	}
	for _, t := range wsTypesNamedInGoal(goal) {
		add(t)
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
		if slices.Contains(out, f) {
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

func wsCaseID(service, role, typ string) string {
	return "ws-" + service + "-" + role + "-" + sanitizeTypeID(typ)
}

// sanitizeTypeID turns a routing type into an ID-safe token.
func sanitizeTypeID(typ string) string {
	r := strings.NewReplacer(":", "-", "/", "-", " ", "-")
	return r.Replace(typ)
}

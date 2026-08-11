package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// stepEvidence builds one trace entry carrying the structured WS facts
// downstream fan-out derivation needs (connectionID + matched type). Kept as a
// helper so the field population is unit-testable without a live WS server.
func stepEvidence(s TestStep, result types.ExecutorResult) Evidence {
	ev := Evidence{
		Type:         evidenceType(result),
		Content:      fmt.Sprintf("%s: %s", s.Action, result.Summary()),
		Action:       s.Action,
		ConnectionID: s.ConnectionID,
	}
	if s.Action == "ws_receive" {
		ev.MatchedType = s.Type
		ev.Matched = wsReceiveMatched(result)
		ev.ExpectAbsent = s.ExpectAbsent
	}
	if s.Action == "ws_send" {
		ev.MatchedType = typeOfSend(s.Message)
	}
	if s.Action == "http_request" {
		if hr, ok := result.(types.HTTPResult); ok {
			ev.Content = fmt.Sprintf("http_request: %s %d", hr.URL, hr.StatusCode)
		}
	}
	return ev
}

// wsReceiveMatched reports whether a ws_receive result actually observed a
// matching frame (MatchedCount>0 or a non-empty MatchedMessage), distinct from
// Success() which can be true for a non-decisive receive.
func wsReceiveMatched(result types.ExecutorResult) bool {
	if wr, ok := result.(types.WSResult); ok {
		return wr.MatchedCount > 0 || wr.MatchedMessage != ""
	}
	return false
}

// typeOfSend best-effort extracts the "type" field from a ws_send JSON message
// so fan-out can correlate sender and recipients by message type.
func typeOfSend(msg string) string {
	var m map[string]any
	if json.Unmarshal([]byte(msg), &m) == nil {
		if t, ok := m["type"].(string); ok {
			return t
		}
	}
	return ""
}

// stepToAction converts a declarative TestStep into the typed WS action the
// shared executor already dispatches. Every step carries its own connection_id,
// so a case may address several connections. The connect step dials s.URL when
// set, otherwise tc.Target — so a single case can dial peers at different
// endpoints (cross-endpoint multi-party relay). Role drives protocol auth +
// handshake exactly as a Steer-emitted ws_connect.
func stepToAction(tc *TestCase, s TestStep) (types.TypedAction, error) {
	switch s.Action {
	case "ws_connect":
		url := s.URL
		if url == "" {
			url = tc.Target
		}
		return types.WSConnectAction{URL: url, Role: s.Role, ConnectionID: s.ConnectionID}, nil
	case "ws_send":
		return types.WSSendAction{ConnectionID: s.ConnectionID, Message: s.Message}, nil
	case "ws_receive":
		return types.WSReceiveAction{ConnectionID: s.ConnectionID, Type: s.Type,
			Aliases: s.Aliases, Assert: s.Asserts, Timeout: s.Timeout, Decisive: true, MatchAll: s.MatchAll,
			ExpectAbsent: s.ExpectAbsent}, nil
	case "ws_disconnect":
		return types.WSDisconnectAction{ConnectionID: s.ConnectionID}, nil
	default:
		return nil, fmt.Errorf("steps: unknown action %q", s.Action)
	}
}

// resolveHTTPStep turns an http_request TestStep into a dispatchable HTTPAction.
// URL and Body {{param}}/{{role.param}} placeholders resolve from provisioned
// actor state (resolvePlaceholders); AuthRole's actor HTTP token is injected as
// "Authorization: Bearer <token>" unless an explicit Authorization header is
// present (explicit headers win). The protocol is looked up by the URL host.
func resolveHTTPStep(idx *WSProtocolIndex, s TestStep) (types.TypedAction, error) {
	method := s.Method
	if method == "" {
		method = "GET"
	}
	proto := protocolForURL(idx, s.URL)
	owningActor := ""
	if proto != nil && s.AuthRole != "" {
		if r := proto.Roles[s.AuthRole]; r != nil {
			owningActor = r.CredentialRef
		}
	}
	resolvedURL, err := resolvePlaceholders(idx, proto, owningActor, s.URL)
	if err != nil {
		return nil, fmt.Errorf("http_request: %w", err)
	}
	body := s.Body
	if body != "" {
		if b, berr := resolvePlaceholders(idx, proto, owningActor, body); berr == nil {
			body = b
		} else {
			return nil, fmt.Errorf("http_request: %w", berr)
		}
	}
	headers := map[string]string{}
	for k, v := range s.Headers {
		headers[k] = v
	}
	if s.AuthRole != "" && owningActor != "" && idx != nil {
		if tok := idx.ActorHTTPTokens[owningActor]; tok != "" {
			if _, set := headers["Authorization"]; !set {
				headers["Authorization"] = "Bearer " + tok
			}
		} else {
			return nil, fmt.Errorf("http_request: no http token for actor %q", owningActor)
		}
	}
	return types.HTTPAction{Method: method, URL: resolvedURL, Headers: headers, Body: body}, nil
}

// protocolForURL returns the declared protocol for a URL's host, or nil.
func protocolForURL(idx *WSProtocolIndex, rawURL string) *project.Protocol {
	if idx == nil {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	return idx.ByHost[u.Host]
}

// suppressAwaitTypes pre-scans a case's Steps and returns, per connection_id,
// the set of message types a ws_receive on that connection will assert (Type +
// Aliases). The deterministic Steps runner attaches this set to each ws_connect
// so an OPTIONAL handshake can skip its connect-time auto-await when a later
// explicit receive is the decisive assertion of the same type — avoiding both a
// pointless full-timeout stall and consuming a frame the later receive needs.
// Receives always follow a connection's connect (one cannot receive on an
// unconnected id), so "all receives on this connection" == "later receives".
func suppressAwaitTypes(steps []TestStep) map[string][]string {
	sets := map[string]map[string]bool{}
	for _, s := range steps {
		if s.Action != "ws_receive" {
			continue
		}
		set, ok := sets[s.ConnectionID]
		if !ok {
			set = map[string]bool{}
			sets[s.ConnectionID] = set
		}
		if s.Type != "" {
			set[s.Type] = true
		}
		for _, a := range s.Aliases {
			if a != "" {
				set[a] = true
			}
		}
	}
	out := make(map[string][]string, len(sets))
	for conn, set := range sets {
		list := make([]string, 0, len(set))
		for t := range set {
			list = append(list, t)
		}
		out[conn] = list
	}
	return out
}

// runSteps executes a deterministic multi-step WS case: each step runs via the
// shared executor under the case context (caseIDKey already set by executeStep).
// Steps citing the SAME connection_id share one connection; steps citing
// DIFFERENT connection_ids open distinct connections in the same case (the table
// is keyed <caseID>:<connectionID>), enabling multi-connection / cross-socket
// relay orchestration. The first failed step short-circuits the case. The
// decisive verdict is the final ws_receive assert; a completed chain is a real
// upgraded exchange for the Examiner.
func (se *stepExecution) runSteps() StepResult {
	r := se.loop
	suppress := suppressAwaitTypes(se.tc.Steps)
	var evidence []Evidence
	var lastAction types.TypedAction
	var lastResult types.ExecutorResult
	for _, s := range se.tc.Steps {
		var action types.TypedAction
		var err error
		if s.Action == "http_request" {
			action, err = resolveHTTPStep(r.wsIdx, s)
		} else {
			action, err = stepToAction(se.tc, s)
		}
		if err != nil {
			return se.failureResult(err, 1)
		}
		// Tell an optional-handshake connect which await-types a later receive on
		// the same connection will assert, so its auto-await can be suppressed.
		if s.Action == "ws_connect" {
			if ca, ok := action.(types.WSConnectAction); ok {
				if suppressed := suppress[s.ConnectionID]; len(suppressed) > 0 {
					ca.SuppressAwaitTypes = suppressed
					action = ca
				}
			}
		}
		result := r.executor.Execute(se.ctx, action)
		r.recordEvidence(se.ctx, se.traceID, "steps", action, result)
		evidence = append(evidence, stepEvidence(s, result))
		lastAction, lastResult = action, result
		// http_request explicit status assertion: when expect_status is set, a
		// non-matching status fails the step regardless of the executor's own
		// success/ok gate.
		if s.Action == "http_request" && s.ExpectStatus != 0 {
			if hr, ok := result.(types.HTTPResult); ok && hr.StatusCode != s.ExpectStatus {
				return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
					Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
			}
		}
		if !result.Success() {
			return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
				Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
		}
	}
	return StepResult{TestCase: se.tc, Status: StepPassed, TraceID: se.traceID,
		Attempts: 1, Duration: time.Since(se.start), Action: lastAction, Result: lastResult, Evidence: evidence}
}

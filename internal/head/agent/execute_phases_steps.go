package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// runProcessRestart executes a process_restart step: resolve the declared
// Role to its real-process actor (credential_ref) and delegate to the session
// harness via the ActorRestarter hook. The step is only emitted when a
// sacrificial second real bridge exists; a nil hook or failed relaunch fails
// the step with a clear error.
func (se *stepExecution) runProcessRestart(s TestStep) (types.ProcessRestartResult, error) {
	r := se.loop
	start := time.Now()
	fail := func(err error) (types.ProcessRestartResult, error) {
		return types.ProcessRestartResult{OK: false, Actor: s.Role, Latency: time.Since(start), Err: err.Error()}, err
	}
	if r.actorRestart == nil {
		return fail(fmt.Errorf("process_restart: no actor restarter attached (no real-process harness)"))
	}
	actor := s.Role
	if proto := protocolForURL(r.wsIdx, se.tc.Target); proto != nil {
		if role, ok := proto.Roles[s.Role]; ok && role != nil && role.CredentialRef != "" {
			actor = role.CredentialRef
		}
	}
	if err := r.actorRestart.RestartActor(se.ctx, actor); err != nil {
		return fail(err)
	}
	return types.ProcessRestartResult{OK: true, Actor: actor, Latency: time.Since(start)}, nil
}

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
	if s.Action == "browser_expect" {
		ev.MatchedType = s.Type // the assertion id (vocab) — coverage credits on it
		if br, ok := result.(types.BrowserResult); ok {
			ev.Matched = br.OK
			ev.Content = fmt.Sprintf("browser_expect %s: %s", s.Type, result.Summary())
		}
	}
	if s.Action == "ws_receive" {
		ev.MatchedType = s.Type
		ev.Matched = wsReceiveMatched(result)
		ev.ExpectAbsent = s.ExpectAbsent
		if wr, ok := result.(types.WSResult); ok {
			n := wr.MatchedCount
			if n == 0 && ev.Matched {
				n = 1 // non-MatchAll receives count presence, not MatchedCount
			}
			ev.MatchedCount = n
			if len(wr.MatchedMessages) > 0 {
				ev.MatchedOrder = burstOrderKeys(wr.MatchedMessages)
			}
		}
	}
	if s.Action == "ws_send" {
		ev.MatchedType = typeOfSend(s.Message)
	}
	if s.Action == "process_restart" {
		if pr, ok := result.(types.ProcessRestartResult); ok {
			ev.Content = fmt.Sprintf("process_restart: %s", pr.Actor)
		}
	}
	if s.Action == "http_request" {
		if hr, ok := result.(types.HTTPResult); ok {
			ev.Content = fmt.Sprintf("http_request: %s %d", hr.URL, hr.StatusCode)
			ev.URL = hr.URL
			ev.StatusCode = hr.StatusCode
			m := strings.ToUpper(s.Method)
			if m == "" {
				m = "GET"
			}
			ev.Method = m
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

// statusInClass reports whether an HTTP status code falls in a declared class
// ("2xx".."5xx", the compound "2xx_4xx", or "any" = every real status). Status
// 0 (transport error) is in NO class — reachability means a response was
// received. An unknown class name returns an error instead of being silently
// treated as "any".
func statusInClass(class string, code int) (bool, error) {
	switch class {
	case "any":
		return code >= 100 && code <= 599, nil
	case "2xx", "3xx", "4xx", "5xx":
		want := int(class[0]-'0') * 100
		return code >= want && code < want+100, nil
	case "2xx_4xx":
		// Compound class: success OR client error — "no server error, no
		// routing error" (authed mutations accept legitimate 4xx rejections).
		return (code >= 200 && code < 300) || (code >= 400 && code < 500), nil
	default:
		return false, fmt.Errorf("expect_status_class: unknown class %q (want 2xx|3xx|4xx|5xx|2xx_4xx|any)", class)
	}
}

// statusClassError renders the statusClassIn failure reason.
func statusClassError(class string, code int, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("expect_status_class %q: got status %d (transport errors carry status 0)", class, code)
}

// burstOrderKeys extracts one comparable key per matched frame for the
// ordering dimension: the first scalar among seq/id/n found at the frame's
// top level or inside payload (priority seq > id > n per level). When any
// frame lacks a key the WHOLE burst falls back to positional placeholders so
// the rendered order list stays homogeneous — a mixed key/placeholder list
// invites the judge to compare incomparable values.
func burstOrderKeys(frames []string) []string {
	keys := make([]string, len(frames))
	for i, f := range frames {
		k, ok := frameKey(f)
		if !ok {
			for j := range keys {
				keys[j] = fmt.Sprintf("#%d", j)
			}
			return keys
		}
		keys[i] = k
	}
	return keys
}

// frameKey finds the first comparable scalar key (seq/id/n, top level then
// payload) in one JSON frame. Only strings and numbers qualify.
func frameKey(frame string) (string, bool) {
	var m map[string]any
	if json.Unmarshal([]byte(frame), &m) != nil {
		return "", false
	}
	for _, name := range []string{"seq", "id", "n"} {
		if s, ok := scalarKey(m, name); ok {
			return s, true
		}
		if p, ok := m["payload"].(map[string]any); ok {
			if s, ok := scalarKey(p, name); ok {
				return s, true
			}
		}
	}
	return "", false
}

// scalarKey renders m[name] when it is a non-empty string or a number.
func scalarKey(m map[string]any, name string) (string, bool) {
	switch v := m[name].(type) {
	case string:
		if v != "" {
			return v, true
		}
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	}
	return "", false
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
	case "ws_expect_close":
		return types.WSExpectCloseAction{ConnectionID: s.ConnectionID, Code: s.Code, Timeout: s.Timeout}, nil
	default:
		return nil, fmt.Errorf("steps: unknown action %q", s.Action)
	}
}

// browserExpectComparator reads the comparator off a browser_expect step:
// s.Asserts["expectation"] when the step came from YAML/vocab, else
// "text_present" (the overwhelmingly common shape).
func browserExpectComparator(s TestStep) string {
	if v, ok := s.Asserts["expectation"].(string); ok && v != "" {
		return v
	}
	return "text_present"
}

// resolveBrowserStep turns a browser_* TestStep into its typed action. URL
// resolution mirrors ws_connect: an absolute s.URL wins; otherwise it is a
// route joined onto tc.Target (the UI base URL carried by the case).
func resolveBrowserStep(tc *TestCase, s TestStep) (types.TypedAction, error) {
	switch s.Action {
	case "browser_goto":
		url := s.URL
		if url == "" {
			url = tc.Target
		} else if !isURL(url) {
			url = strings.TrimSuffix(tc.Target, "/") + "/" + strings.TrimPrefix(url, "/")
		}
		return types.BrowserGotoAction{URL: url}, nil
	case "browser_click":
		return types.BrowserClickAction{Selector: s.Target}, nil
	case "browser_fill":
		return types.BrowserFillAction{Selector: s.Target, Value: s.Message}, nil
	case "browser_expect":
		return types.BrowserExpectAction{Selector: s.Target,
			Expectation: browserExpectComparator(s), Timeout: s.Timeout}, nil
	default:
		return nil, fmt.Errorf("browser steps: unknown action %q", s.Action)
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

// httpStepPassed evaluates the http_request explicit status gates. When a
// gate applies and passes it is the step's ONLY success criterion — a declared
// rejection (4xx/5xx) whose status matches passes even though the executor's
// own ok gate is false for non-2xx (negative case family). The second return
// is the failure reason to attach to the StepResult when passed is false
// (nil for a plain expect_status mismatch). When no gate applies the
// executor's own success gate decides.
func httpStepPassed(s TestStep, result types.ExecutorResult) (bool, error) {
	if s.ExpectStatus != 0 {
		if hr, ok := result.(types.HTTPResult); ok {
			if hr.StatusCode != s.ExpectStatus {
				return false, nil
			}
			return true, nil
		}
	}
	if s.ExpectStatusClass != "" {
		if hr, ok := result.(types.HTTPResult); ok {
			ok2, cerr := statusInClass(s.ExpectStatusClass, hr.StatusCode)
			if cerr != nil || !ok2 {
				return false, statusClassError(s.ExpectStatusClass, hr.StatusCode, cerr)
			}
			// Any response in the class proves reachability; transport errors
			// (status 0) are in no class and fail above.
			return true, nil
		}
	}
	return result.Success(), nil
}

// stepLogFields collects the fields shared by every per-step info line: the
// case, the 1-based step position, the action, and the step's discriminating
// parameters — enough to tell WHICH step acted and on what from the run log
// alone, without dumping message bodies. Emitted once per executed step
// (open-agents #23 observability: ws_flow cases previously logged only case
// start/completion, so a mid-case failure was invisible at info level).
func (se *stepExecution) stepLogFields(i int, s TestStep) []zap.Field {
	caseID, _ := se.ctx.Value(caseIDKey{}).(string)
	fields := []zap.Field{
		zap.String("case_id", caseID),
		zap.Int("step", i+1),
		zap.String("action", s.Action),
	}
	if s.ConnectionID != "" {
		fields = append(fields, zap.String("connection_id", s.ConnectionID))
	}
	if s.Type != "" {
		fields = append(fields, zap.String("type", s.Type))
	}
	if s.Role != "" {
		fields = append(fields, zap.String("role", s.Role))
	}
	if s.Method != "" {
		fields = append(fields, zap.String("method", s.Method))
	}
	if s.URL != "" {
		fields = append(fields, zap.String("url", s.URL))
	}
	if s.Target != "" {
		fields = append(fields, zap.String("target", s.Target))
	}
	return fields
}

// logStep emits the per-step info line once the step's outcome is known.
// passed carries the step-level verdict (http gates can override the
// executor's own success gate); result may be nil for pre-execution failures
// (step resolution), which is exactly the blind spot this line closes.
func (se *stepExecution) logStep(i int, s TestStep, result types.ExecutorResult, latency time.Duration, passed bool, err error) {
	fields := append(se.stepLogFields(i, s),
		zap.Bool("passed", passed),
		zap.Duration("latency", latency),
	)
	if result != nil {
		fields = append(fields, zap.String("summary", truncateStr(result.Summary(), 200)))
		if hr, ok := result.(types.HTTPResult); ok {
			fields = append(fields, zap.Int("status_code", hr.StatusCode))
		}
	}
	if err != nil {
		fields = append(fields, zap.Error(err))
	}
	se.loop.logger.Info("case step", fields...)
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
	if se.caseParams == nil {
		se.caseParams = map[string]string{}
	}
	var evidence []Evidence
	var lastAction types.TypedAction
	var lastResult types.ExecutorResult
	for i, s := range se.tc.Steps {
		s = substituteCaseParams(s, se.caseParams)
		stepStart := time.Now()
		var action types.TypedAction
		var err error
		if s.Action == "process_restart" {
			// Not a typed executor action: the harness lives at the session
			// layer. Role names the DECLARED protocol role; its credential_ref
			// resolves to the real-process actor to relaunch. No
			// recordEvidence here — it requires a TypedAction; the step's
			// Evidence entry below carries the facts.
			result, rerr := se.runProcessRestart(s)
			evidence = append(evidence, stepEvidence(s, result))
			se.logStep(i, s, result, time.Since(stepStart), rerr == nil, rerr)
			if rerr != nil || !result.Success() {
				return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
					Attempts: 1, Duration: time.Since(se.start), Result: result, Evidence: evidence,
					Error: rerr}
			}
			continue
		}
		if s.Action == "http_request" {
			action, err = resolveHTTPStep(r.wsIdx, s)
		} else if strings.HasPrefix(s.Action, "browser_") {
			if s.Action == "browser_shot" {
				// Not a typed executor action: capture via the executor's
				// file sink; the Evidence entry carries the path.
				if be := r.browserExec(); be != nil {
					caseID, _ := se.ctx.Value(caseIDKey{}).(string)
					if p, serr := be.ScreenshotToFile(caseID, s.Label); serr == nil {
						evidence = append(evidence, Evidence{Type: "browser_shot",
							Content: fmt.Sprintf("browser_shot: %s", p), Action: s.Action})
						se.logStep(i, s, nil, time.Since(stepStart), true, nil)
					} else {
						err := fmt.Errorf("browser_shot: %w", serr)
						se.logStep(i, s, nil, time.Since(stepStart), false, err)
						return se.failureResult(err, 1)
					}
				} else {
					err := fmt.Errorf("browser_shot: browser executor unavailable")
					se.logStep(i, s, nil, time.Since(stepStart), false, err)
					return se.failureResult(err, 1)
				}
				continue
			}
			action, err = resolveBrowserStep(se.tc, s)
		} else {
			action, err = stepToAction(se.tc, s)
		}
		if err != nil {
			// Resolution failed before the executor ran: no evidence row is
			// written for this step, so this log line is the ONLY trace of it.
			se.logStep(i, s, nil, time.Since(stepStart), false, err)
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
		if s.Action == "http_request" {
			passed, gateErr := httpStepPassed(s, result)
			se.logStep(i, s, result, time.Since(stepStart), passed, gateErr)
			if !passed {
				return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
					Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence,
					Error: gateErr}
			}
			// Capture dot-paths from the response body into per-case params so
			// later steps can consume server-generated ids ({{case.<name>}}).
			// Missing paths are hard errors — a silently-wrong later request is
			// worse than a clear failure here. The one exception is an EMPTY
			// list: the chain is live-correct, there is just nothing to chain
			// from, so the case ends as a skip and the target step never runs.
			if len(s.Capture) > 0 {
				if hr, ok := result.(types.HTTPResult); ok {
					captured, err := captureFromHTTPBody(hr.Body, s.Capture)
					if err != nil {
						se.logStep(i, s, result, time.Since(stepStart), false, err)
						if errors.Is(err, ErrEmptyListCapture) {
							return StepResult{TestCase: se.tc, Status: StepSkipped, TraceID: se.traceID,
								Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence,
								Error: err}
						}
						return se.failureResult(err, 1)
					}
					maps.Copy(se.caseParams, captured)
				}
			}
			continue
		}
		se.logStep(i, s, result, time.Since(stepStart), result.Success(), nil)
		if !result.Success() {
			if strings.HasPrefix(s.Action, "browser_") {
				// Auto-capture on failure (spec §6): the DOM excerpt rides the
				// result; the screenshot rides the shots dir. Best-effort — the
				// failure itself must still propagate.
				if be := r.browserExec(); be != nil {
					caseID, _ := se.ctx.Value(caseIDKey{}).(string)
					_, _ = be.ScreenshotToFile(caseID, "fail")
				}
			}
			return StepResult{TestCase: se.tc, Status: StepFailed, TraceID: se.traceID,
				Attempts: 1, Duration: time.Since(se.start), Action: action, Result: result, Evidence: evidence}
		}
	}
	return StepResult{TestCase: se.tc, Status: StepPassed, TraceID: se.traceID,
		Attempts: 1, Duration: time.Since(se.start), Action: lastAction, Result: lastResult, Evidence: evidence}
}

package scout

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// caseAssembler is the mutable state assemblePlan threads through its tool-call
// loop: the accumulated cases, the currently-open ws_flow case, and three side
// tables (covered roles, the sound case that covered each role, HTTP endpoints
// covered) that WSCasesCovered / HTTPCasesCovered read to suppress redundant
// deterministic fallbacks. Hoisting this state onto a struct lets the ws_flow
// flush logic live in a named method instead of a closure capturing five locals.
type caseAssembler struct {
	cases        []agent.TestCase
	open         *agent.TestCase
	covered      map[string]map[string]bool
	coveringCase map[string]map[string]string
	httpCovering map[string]map[string]string
	svcProtos    map[string]*project.Protocol
	services     []project.Service
	goal         string
	id           int
}

func newCaseAssembler(goal string, services []project.Service) *caseAssembler {
	c := &caseAssembler{
		covered:      map[string]map[string]bool{},
		coveringCase: map[string]map[string]string{},
		httpCovering: map[string]map[string]string{},
		svcProtos:    map[string]*project.Protocol{},
		services:     services,
		goal:         goal,
	}
	// service -> declared protocol, so flush can judge ws_flow soundness without
	// re-scanning services per case.
	for _, s := range services {
		if s.Protocol != nil {
			c.svcProtos[s.Name] = s.Protocol
		}
	}
	return c
}

func (c *caseAssembler) nextID() string {
	c.id++
	return fmt.Sprintf("tc-%03d", c.id)
}

// finalizeOpen flushes the open ws_flow case (if any). A begin_case the LLM
// opened with no following ws_* calls is dropped (a 0-step ws_flow is not a real
// case — it would waste an Agent cycle and confuse the Examiner). Otherwise the
// case is sanitized (self-handshake re-await), judged for soundness, and only a
// SOUND case marks its connected roles covered (so WSCasesCovered still emits
// the deterministic fallback for unsound roles). The case is appended either
// way; soundness only affects the coverage side table.
func (c *caseAssembler) finalizeOpen() {
	if c.open == nil {
		return
	}
	open := c.open
	if open.Action == "ws_flow" && len(open.Steps) == 0 {
		// A begin_case the LLM opened with no following ws_* calls. Drop it.
		c.open = nil
		return
	}
	// ws self-handshake re-await: a ws_connect auto-awaits and consumes its
	// role's Handshake.AwaitType frame (websocket.go readMatching), so a later
	// ws_receive of that type on the same connection would re-await an
	// already-consumed frame and time out. The deterministic emitter excludes
	// this at emit time (ws_cases.go); mirror that for LLM-authored ws_flow
	// cases before soundness/coverage is judged.
	sanitizeSelfHandshakeReawait(open, c.svcProtos[open.Service])
	if open.Service != "" {
		// A1 unsound-fallback (Phase 1): only a SOUND ws_flow suppresses the
		// deterministic fallback for the roles it connects. An unsound case (a
		// ws_receive of an invented type) stays in the plan but does not mark
		// its roles covered, so WSCasesCovered still emits the deterministic
		// fallback for them.
		if llmWSFlowSound(open, c.svcProtos[open.Service], c.goal) {
			for _, st := range open.Steps {
				if st.Action == "ws_connect" && st.Role != "" {
					if c.covered[open.Service] == nil {
						c.covered[open.Service] = map[string]bool{}
					}
					c.covered[open.Service][st.Role] = true
					// A1 Phase 2: record which sound case covered this role, so
					// WSCasesCovered can bind a lazy fallback.
					if c.coveringCase[open.Service] == nil {
						c.coveringCase[open.Service] = map[string]string{}
					}
					c.coveringCase[open.Service][st.Role] = open.ID
				}
			}
		}
	}
	c.cases = append(c.cases, *open)
	c.open = nil
}

// handle dispatches one directPlan tool call. Unknown/invalid calls are no-ops.
func (c *caseAssembler) handle(call llm.ToolCall) {
	switch call.Name {
	case "test_http_endpoint":
		c.finalizeOpen()
		hc := assembleHTTP(call, c.nextID, c.services)
		if hc.Service != "" && strings.HasPrefix(hc.Target, "/") {
			if c.httpCovering[hc.Service] == nil {
				c.httpCovering[hc.Service] = map[string]string{}
			}
			if _, dup := c.httpCovering[hc.Service][hc.Target]; !dup {
				c.httpCovering[hc.Service][hc.Target] = hc.ID
			}
		}
		c.cases = append(c.cases, hc)
	case "check_invariant":
		c.finalizeOpen()
		c.cases = append(c.cases, assembleInvariant(call, c.nextID))
	case "run_process":
		c.finalizeOpen()
		c.cases = append(c.cases, assembleProcess(call, c.nextID))
	case "analyze_code":
		c.finalizeOpen()
		c.cases = append(c.cases, assembleCode(call, c.nextID))
	case "check_file":
		c.finalizeOpen()
		c.cases = append(c.cases, assembleFile(call, c.nextID))
	case "navigate":
		c.finalizeOpen()
		c.cases = append(c.cases, assembleNavigate(call, c.nextID))
	case "begin_case":
		c.finalizeOpen()
		svcName := llm.StrField(call, "service")
		c.open = &agent.TestCase{
			ID: c.nextID(), Name: llm.StrField(call, "name"),
			Expectation: llm.StrField(call, "expectation"), Action: "ws_flow",
			Service: svcName,
			// ws_connect dials tc.Target (stepToAction); the LLM emits the
			// service NAME, so resolve it to the service URL here.
			Target: serviceURLByName(svcName, c.services),
		}
	case "ws_connect":
		if c.open == nil {
			return
		}
		c.open.Steps = append(c.open.Steps, agent.TestStep{
			Action: "ws_connect", ConnectionID: llm.StrField(call, "role"), Role: llm.StrField(call, "role"),
			URL: llm.StrField(call, "url"),
		})
	case "ws_send":
		if c.open == nil {
			return
		}
		c.open.Steps = append(c.open.Steps, agent.TestStep{
			Action: "ws_send", ConnectionID: llm.StrField(call, "role"), Message: wsSendBody(llm.StrField(call, "type"), nil),
		})
	case "ws_receive":
		if c.open == nil {
			return
		}
		c.open.Steps = append(c.open.Steps, agent.TestStep{
			Action: "ws_receive", ConnectionID: llm.StrField(call, "role"),
			Type: llm.StrField(call, "type"), Aliases: llm.StrSliceField(call, "aliases"),
			Asserts: llm.MapField(call, "assert"), Timeout: llm.IntField(call, "timeout"),
		})
	case "ws_disconnect":
		if c.open == nil {
			return
		}
		c.open.Steps = append(c.open.Steps, agent.TestStep{
			Action: "ws_disconnect", ConnectionID: llm.StrField(call, "role"),
		})
	}
}

// assemblePlan converts directPlan tool calls into a TestPlan plus the
// per-service set of roles already connected by a begin_case+ws_* group
// (covered), so WSCasesCovered can suppress redundant deterministic connects.
// Unknown/invalid calls are dropped, never panic.
func assemblePlan(calls []llm.ToolCall, goal, baseURL string, services []project.Service) (*agent.TestPlan, map[string]map[string]bool, map[string]map[string]string, map[string]map[string]string) {
	c := newCaseAssembler(goal, services)
	for _, call := range calls {
		c.handle(call)
	}
	c.finalizeOpen()
	cases := fillBody(c.cases, services) // retained: service.BodyTemplate fill
	return &agent.TestPlan{Goal: goal, Cases: cases, ProjectURL: baseURL}, c.covered, c.coveringCase, c.httpCovering
}

// --- field helpers live in internal/llm/toolfield.go (shared with Agent) ---

// assembleAnalyze converts Analyze tool calls (report_endpoint/report_page/
// declare_tech) into an AnalyzeOutput. The provider schema enforces string
// arrays for declare_tech, so no drift absorption is needed. Unknown calls are
// dropped, never panic.
func assembleAnalyze(calls []llm.ToolCall) AnalyzeOutput {
	var out AnalyzeOutput
	for _, c := range calls {
		switch c.Name {
		case "report_endpoint":
			out.Endpoints = append(out.Endpoints, EndpointInfo{
				Method:     llm.StrField(c, "method"),
				Path:       llm.StrField(c, "path"),
				Confidence: llm.NumField(c, "confidence"),
			})
		case "report_page":
			out.Pages = append(out.Pages, PageInfo{
				Path:       llm.StrField(c, "path"),
				Confidence: llm.NumField(c, "confidence"),
			})
		case "declare_tech":
			out.TechStack = llm.StrSliceField(c, "stack")
		}
	}
	return out
}

// assembleContract converts coverage-contract tool calls (declare_scope/
// path_types/error_scope/boundaries, set_priority, set_coverage_gate) into a
// contract.Contract. Priorities is initialized non-nil so set_priority can
// populate it; the set_priority schema forces map[string][]string, so no
// Priorities.UnmarshalJSON dual-shape absorption is needed. Unknown calls are
// dropped, never panic.
//
// hasVocab marks contracts for services that declare a WS/SaaS vocabulary. For
// those, the LLM's set_coverage_gate (module/line/branch) is meaningless —
// there is no local SUT module for a SaaS service — so it is skipped and an
// objective PathThreshold=1.0 gate is set instead (every declared message edge
// must be exercised). When hasVocab is false, behavior is byte-identical to the
// pre-vocab local-codebase contract.
func assembleContract(calls []llm.ToolCall, depth string, invs []contract.InvariantRef, hasVocab bool) *contract.Contract {
	c := &contract.Contract{Depth: depth, Priorities: contract.Priorities{}, Invariants: invs}
	for _, call := range calls {
		switch call.Name {
		case "declare_scope":
			c.Scope = llm.StrSliceField(call, "modules")
		case "declare_path_types":
			c.PathTypes = llm.StrSliceField(call, "types")
		case "declare_error_scope":
			c.ErrorScope = llm.StrSliceField(call, "scopes")
		case "declare_boundaries":
			c.Boundaries = llm.StrSliceField(call, "boundaries")
		case "set_priority":
			c.Priorities[llm.StrField(call, "bucket")] = llm.StrSliceField(call, "modules")
		case "set_coverage_gate":
			if hasVocab {
				// SaaS/WS service: the LLM's module/line/branch gate is
				// meaningless (no local SUT). Use the objective path gate;
				// every declared message edge must be exercised. The authority
				// surface is the extracted vocabulary, not the LLM's guess.
				continue
			}
			c.CoverageGate = contract.Gate{
				Module:          llm.StrField(call, "module"),
				LineThreshold:   llm.NumField(call, "line_threshold"),
				BranchThreshold: llm.NumField(call, "branch_threshold"),
			}
		}
	}
	if hasVocab {
		// SaaS/WS service: every declared message edge must be exercised. The
		// authority surface is the extracted vocabulary, not the LLM's guess.
		c.CoverageGate.PathThreshold = 1.0
	}
	return c
}

// sanitizeSelfHandshakeReawait drops redundant ws_receive steps that re-await
// a MANDATORY handshake frame a ws_connect on the same connection has already
// auto-consumed (a mandatory connect's auto-await succeeds within the handshake
// window, consuming the AwaitType frame; a later receive of that type would
// time out). The deterministic emitter excludes these at emit time
// (ws_cases.go); this mirrors that defense for LLM-authored ws_flow cases.
//
// OPTIONAL handshakes are intentionally NOT sanitized: an optional connect's
// auto-await TIMES OUT (the AwaitType is a peer-join signal that arrives later,
// beyond the handshake window) without consuming the frame, so the later
// ws_receive(signal) is the decisive peer-join assertion and MUST stay. The
// deterministic relay (wsRelayCases) is built only for optional handshakes
// (ws_cases.go:206 — !a.Handshake.Optional → skip) on the same premise.
//
// A ws_connect with no role, a role without a mandatory handshake, or a missing
// protocol makes the call a no-op. When every receive on a connection is
// dropped, the case naturally collapses to connect-only, which llmWSFlowSound
// already accepts as trivially sound (the connect alone proves connect +
// handshake), so the role stays covered.
func sanitizeSelfHandshakeReawait(open *agent.TestCase, proto *project.Protocol) {
	if proto == nil || len(proto.Roles) == 0 || open == nil {
		return
	}
	// connection_id -> sanitized mandatory handshake await type already
	// consumed by the connect on that connection.
	consumed := map[string]string{}
	for _, st := range open.Steps {
		if st.Action != "ws_connect" || st.Role == "" {
			continue
		}
		role, ok := proto.Roles[st.Role]
		if !ok || role == nil || role.Handshake == nil {
			continue
		}
		// Only mandatory handshakes consume the AwaitType frame within the
		// connect; optional handshakes time out and leave it for a later
		// receive to assert.
		if role.Handshake.AwaitType == "" || role.Handshake.Optional {
			continue
		}
		consumed[st.ConnectionID] = sanitizeTypeID(role.Handshake.AwaitType)
	}
	if len(consumed) == 0 {
		return
	}
	kept := make([]agent.TestStep, 0, len(open.Steps))
	for _, st := range open.Steps {
		if st.Action == "ws_receive" {
			if hid, ok := consumed[st.ConnectionID]; ok {
				if sanitizeTypeID(st.Type) == hid {
					continue
				}
				drop := false
				for _, a := range st.Aliases {
					if sanitizeTypeID(a) == hid {
						drop = true
						break
					}
				}
				if drop {
					continue
				}
			}
		}
		kept = append(kept, st)
	}
	open.Steps = kept
}

// --- high-level assemblers ---

// serviceURLByName returns the URL of the named service, or "" if not found.
// Used by begin_case so a ws_flow case's Target is the dial URL stepToAction
// uses for ws_connect (the LLM emits the service NAME, not the URL).
func serviceURLByName(name string, services []project.Service) string {
	for _, s := range services {
		if s.Name == name {
			return s.URL
		}
	}
	return ""
}

func assembleHTTP(c llm.ToolCall, nextID func() string, svcs []project.Service) agent.TestCase {
	method := strings.ToUpper(llm.StrField(c, "method"))
	path := llm.StrField(c, "path")
	tc := agent.TestCase{
		ID: nextID(), Name: fmt.Sprintf("%s %s", method, path), Target: path,
		Method: method, Body: llm.StrField(c, "body"),
		Expectation: formatHTTPExpectation(c), Service: llm.StrField(c, "service"),
	}
	if svc := attributeService(path, svcs); svc != "" {
		tc.Service = svc // deterministic override (replaces verifyServiceAttribution)
	}
	return tc
}

func formatHTTPExpectation(c llm.ToolCall) string {
	var parts []string
	if s := llm.IntField(c, "expect_status"); s != 0 {
		parts = append(parts, fmt.Sprintf("status %d", s))
	}
	if b := llm.StrField(c, "expect_body"); b != "" {
		parts = append(parts, fmt.Sprintf("body contains %q", b))
	}
	if len(parts) == 0 {
		return "Returns 2xx status code"
	}
	return strings.Join(parts, "; ")
}

func assembleInvariant(c llm.ToolCall, nextID func() string) agent.TestCase {
	desc := llm.StrField(c, "description")
	if id := llm.StrField(c, "invariant_id"); id != "" {
		desc = id
	}
	return agent.TestCase{
		ID: nextID(), Target: desc, Expectation: llm.StrField(c, "assertion"),
		Severity: llm.StrField(c, "severity"),
	}
}

func assembleProcess(c llm.ToolCall, nextID func() string) agent.TestCase {
	a := llm.StrField(c, "action") // build | exec (schema-enforced; test/lint via exec+cmd)
	return agent.TestCase{ID: nextID(), Action: "process_" + a, Target: llm.StrField(c, "cmd"), Expectation: llm.StrField(c, "expect")}
}

func assembleCode(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "code_" + llm.StrField(c, "action"), Target: llm.StrField(c, "target")}
}

func assembleFile(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "file_" + llm.StrField(c, "action"), Target: llm.StrField(c, "path"), Body: llm.StrField(c, "pattern"), Expectation: llm.StrField(c, "expect")}
}

func assembleNavigate(c llm.ToolCall, nextID func() string) agent.TestCase {
	return agent.TestCase{ID: nextID(), Action: "navigate", Target: llm.StrField(c, "path"), Expectation: llm.StrField(c, "expect")}
}

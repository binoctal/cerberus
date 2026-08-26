package session

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/autotest"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/head/contract"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/project"
)

// lineCoverage returns the Examiner-phase coverage measurement, reusing
// lineCoverageReport so the provider runs once and only the measurement is
// projected out (no regression to assessCoverageIfContract).
func (s *Session) lineCoverage(ctx context.Context) contract.CoverageMeasurement {
	_, m := s.lineCoverageReport(ctx)
	return m
}

// lineCoverageReport runs the Examiner-phase coverage provider ONCE and returns
// BOTH the raw CoverageReport (reused by the coverage repair loop for gap
// detection) and the derived CoverageMeasurement. It honors an injected
// override (tests): when coverageFn is set it returns (nil, measurement) — the
// stub supplies only a measurement, and callers tolerate a nil report.
func (s *Session) lineCoverageReport(ctx context.Context) (*autotest.CoverageReport, contract.CoverageMeasurement) {
	if s.coverageFn != nil {
		return nil, s.coverageFn(ctx, s)
	}
	return coverageReportForSession(ctx, s)
}

// assessCoverageIfContract runs the objective coverage assessment against the
// session's contract (if any). Shared by the run and resume Examiner paths.
// sess.lineCoverage honors an injected stub (tests) to avoid recursively
// running go test/jest/pytest when ProjectDir is a module under test.
func assessCoverageIfContract(ctx context.Context, sess *Session, examinerHead *examiner.Examiner, results []agent.StepResult) {
	if sess.Contract == nil {
		return
	}
	// Route to path coverage when any service declares a WS vocabulary; otherwise
	// fall back to line/function coverage. Confined here so the Examiner phase
	// call site needs no branching.
	var measurement contract.CoverageMeasurement
	if sessionHasVocab(sess) {
		measurement = pathCoverage(results, requiredEdges(sess), realProcessRoles(sess.Config))
	} else {
		measurement = sess.lineCoverage(ctx)
	}
	assessment, err := examinerHead.AssessCoverage(ctx, sess.Contract, results, measurement)
	if err == nil {
		sess.Assessment = assessment
		if sessionHasVocab(sess) {
			// Per-edge path gaps (informational, Kind="path" — NOT "coverage",
			// so they do NOT feed the coverage repair loop). AssessCoverage
			// emits the headline "<pct>% exercised < gate" gap when below
			// PathThreshold; here we name each specific unexercised required
			// edge so the report is actionable.
			req := requiredEdges(sess)
			exercised, _ := exercisedEdges(results, req, realProcessRoles(sess.Config))
			for _, e := range req {
				if !exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] {
					sess.Assessment.Gaps = append(sess.Assessment.Gaps, contract.Gap{
						Kind:   "path",
						Detail: fmt.Sprintf("edge %s→%s %s not exercised", originLabel(e), e.ToRole, e.Type),
					})
				}
			}
		}
		if !assessment.Measured {
			sess.Logger.Info("coverage not applicable",
				zap.String("reason", "no measurable local SUT (SaaS/WS session); outcome is verdict-based"))
		} else {
			sess.Logger.Info("coverage assessment",
				zap.Bool("reached", assessment.Reached),
				zap.Int("gaps", len(assessment.Gaps)),
				zap.Float64("coverage_pct", assessment.CoveragePct))
		}
	} else {
		sess.Logger.Warn("coverage assessment failed", zap.Error(err))
	}
}

// coverageForSession returns only the measurement, delegating to
// coverageReportForSession so the provider runs once. Kept as a thin wrapper
// for existing callers/tests.
func coverageForSession(ctx context.Context, sess *Session) contract.CoverageMeasurement {
	_, m := coverageReportForSession(ctx, sess)
	return m
}

// coverageReportForSession runs the language-specific coverage provider and
// returns BOTH the raw report (for gap reuse) and the measurement. Pct is
// normalized to a 0–1 fraction (matching Gate.LineThreshold). Known is true
// only when the provider succeeded and the coverage denominator is non-zero; a
// provider error yields Known=false so the objective gate is skipped instead of
// forcing a false not-reached on a fake 0.
func coverageReportForSession(ctx context.Context, sess *Session) (*autotest.CoverageReport, contract.CoverageMeasurement) {
	provider := coverageProviderForSession(sess)
	report, err := provider.RunCoverage(ctx, sess.ProjectDir)
	if err != nil || report == nil {
		return nil, contract.CoverageMeasurement{Known: false}
	}
	return report, measurementFromReport(report)
}

// detectLanguage identifies the project language from projectDir via package
// markers and a source-file extension. Shared by the coverage provider path
// and the coverage repair axis so language detection is not duplicated.
func detectLanguage(projectDir string) string {
	markers := make(map[string]bool)
	if _, err := os.Stat(filepath.Join(projectDir, "package.json")); err == nil {
		markers["package.json"] = true
	}
	if _, err := os.Stat(filepath.Join(projectDir, "requirements.txt")); err == nil {
		markers["requirements.txt"] = true
	}
	if _, err := os.Stat(filepath.Join(projectDir, "pyproject.toml")); err == nil {
		markers["pyproject.toml"] = true
	}

	var sourceFile string
	if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.go")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.js")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.ts")); len(matches) > 0 {
		sourceFile = matches[0]
	} else if matches, _ := filepath.Glob(filepath.Join(projectDir, "*.py")); len(matches) > 0 {
		sourceFile = matches[0]
	}
	return autotest.DetectLanguage(sourceFile, markers)
}

// coverageProviderForSession builds the language-specific coverage provider
// for a session (nil runner; RunCoverage is only used on the measure paths).
func coverageProviderForSession(sess *Session) autotest.CoverageProvider {
	return autotest.NewCoverageProviderForLanguage(detectLanguage(sess.ProjectDir), nil, sess.Logger)
}

// measurementFromReport derives the normalized CoverageMeasurement from a raw
// provider report. Unit is "line" (Go) or "function" (Node/Python); Pct is a
// 0–1 fraction; Known is false when nothing measurable was collected.
func measurementFromReport(report *autotest.CoverageReport) contract.CoverageMeasurement {
	unit := report.CoverageUnit
	if unit == "" {
		unit = "function"
	}
	var pct100 float64
	known := false
	if unit == "line" {
		// Line coverage is measured when any profile block exists.
		if len(report.Profile) > 0 {
			pct100 = report.LineCoveragePct
			known = true
		}
	} else {
		if report.TotalFuncs > 0 {
			pct100 = float64(report.CoveredFuncs) / float64(report.TotalFuncs) * 100
			known = true
		}
	}
	if !known {
		return contract.CoverageMeasurement{Known: false}
	}
	return contract.CoverageMeasurement{Pct: pct100 / 100, Unit: unit, Known: true}
}

// edgeKey is the stable identity of a vocab message edge.
func edgeKey(from, to, typ string) string { return from + "|" + to + "|" + typ }

// routePatternMatches reports whether a concrete request path matches a
// Hono route pattern: :param consumes exactly one segment; a lone trailing *
// consumes one-or-more. Paths are pre-stripped of query strings.
func routePatternMatches(pattern, p string) bool {
	ps := strings.Split(strings.Trim(pattern, "/"), "/")
	vs := strings.Split(strings.Trim(p, "/"), "/")
	for i, seg := range ps {
		if seg == "*" {
			return i < len(vs) // * is final; needs at least one segment
		}
		if i >= len(vs) {
			return false
		}
		if strings.HasPrefix(seg, ":") {
			continue
		}
		if seg != vs[i] {
			return false
		}
	}
	return len(ps) == len(vs)
}

// routeMethodMatches: ALL (Hono app.all) matches any method.
func routeMethodMatches(declared, actual string) bool {
	return declared == "ALL" || declared == actual
}

// originLabel returns the edge's origin role for gap-detail display. A
// synthesized HTTP-triggered server-push edge has no sender role (empty
// FromRole); render it as "server" instead of an empty segment.
func originLabel(e project.VocabEdge) string {
	if e.FromRole == "" {
		return "server"
	}
	return e.FromRole
}

// exercisedEdges computes which declared message edges a session's results
// exercised using RECEIVE-DRIVEN, vocab-attributed correlation: per case,
// connectionID→role is mapped from that case's ws_connect steps; a matched
// ws_receive of type T by role Rr exercises the declared vocab edge(s)
// (FromRole→Rr, T). The vocab's FromRole is the authority on who sends T to Rr.
//
// This is faithful to push protocols (e.g. open-agents), where a bridge→web
// signal like device:online is SERVER-PUSHED when a peer joins — there is no
// explicit ws_send of it — so a send-side correlation model (ws_send T from Rs
// + ws_receive T by Rr) would measure 0. Negative probes (ExpectAbsent) and
// unmatched receives do not count; connections with no resolvable role are
// excluded. Only edges present in `required` can be attributed (conservative —
// out-of-band receives of undeclared (Rr, T) pairs count nothing). Returns the
// exercised set (keyed edgeKey) and a nil connRole map (reserved). Pure;
// unit-testable without a live server.
//
// Real-recipient amendment: when the declared ToRole is backed by a
// real-process actor (realRoles), its receive can never be observed — cerberus
// does not own that socket. Such edges credit SEND-SIDE: a ws_send of T from a
// connection whose role equals the declared FromRole. The sender side is the
// only observable face of a real recipient. Emulated recipients keep the
// strict receive-driven rule (a send alone proves nothing about delivery).
func exercisedEdges(results []agent.StepResult, required []project.VocabEdge, realRoles map[string]bool) (map[string]bool, map[string]string) {
	// (ToRole, Type) → edge keys declared in the vocab for that recipient+type.
	byToType := map[string][]string{}
	// (FromRole, Type) → edge keys whose recipient is real (send-side credit).
	byFromTypeReal := map[string][]string{}
	for _, e := range required {
		k := edgeKey(e.FromRole, e.ToRole, e.Type)
		byToType[e.ToRole+"|"+e.Type] = append(byToType[e.ToRole+"|"+e.Type], k)
		if realRoles[e.ToRole] {
			byFromTypeReal[e.FromRole+"|"+e.Type] = append(byFromTypeReal[e.FromRole+"|"+e.Type], k)
		}
	}
	exercised := map[string]bool{}
	for _, r := range results {
		// connectionID → role for THIS case (roles are case-scoped via connect steps).
		connRole := map[string]string{}
		for _, s := range r.TestCase.Steps {
			if s.Action == "ws_connect" && s.Role != "" {
				connRole[s.ConnectionID] = s.Role
			}
		}
		for _, ev := range r.Evidence {
			// browser_expect evidence credits the matching ui_assert edge:
			// MatchedType carries the assertion id (vocab cases set it), and
			// only a matched (passed) assertion proves the display promise.
			if ev.Action == "browser_expect" {
				if !ev.Matched || ev.MatchedType == "" {
					continue
				}
				for _, e := range required {
					if e.Trigger == "ui_assert" && e.Type == "ui_assert "+ev.MatchedType {
						exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] = true
					}
				}
				continue
			}
			// http_request steps credit routes directly: any response status
			// proves the route was reached. Rule-engine HTTP evidence lacks
			// structured Method/URL and is not attributed.
			if ev.Action == "http_request" {
				if ev.Method == "" || ev.URL == "" {
					continue
				}
				if u, perr := url.Parse(ev.URL); perr == nil {
					for _, e := range required {
						if e.Trigger != "http_request" || e.FromRole != "http_client" {
							continue
						}
						parts := strings.SplitN(e.Type, " ", 2)
						if len(parts) != 2 {
							continue
						}
						if routeMethodMatches(parts[0], ev.Method) && routePatternMatches(parts[1], u.Path) {
							exercised[edgeKey(e.FromRole, e.ToRole, e.Type)] = true
						}
					}
				}
				continue
			}
			// Send-side credit: only for edges whose recipient is a real
			// process (see the comment above for why this is sound).
			if ev.Action == "ws_send" && ev.MatchedType != "" {
				sender := connRole[ev.ConnectionID]
				if sender == "" {
					continue
				}
				for _, k := range byFromTypeReal[sender+"|"+ev.MatchedType] {
					exercised[k] = true
				}
				continue
			}
			// Only a POSITIVE, matched receive attributes an edge. Sends are not
			// consulted for emulated recipients: the declared FromRole (not an
			// explicit ws_send) identifies the sender, so push signals are captured.
			if ev.Action != "ws_receive" || ev.ExpectAbsent || !ev.Matched || ev.MatchedType == "" {
				continue
			}
			recipient := connRole[ev.ConnectionID] // Rr — the role that observed T
			if recipient == "" {
				continue
			}
			for _, k := range byToType[recipient+"|"+ev.MatchedType] {
				exercised[k] = true
			}
		}
	}
	return exercised, nil
}

// pathCoverage measures message-edge path coverage: exercised / required, over
// the session's declared vocab edges (message_handled, non-unsupported). Known is
// true whenever at least one required edge is declared (a measured 0%, not an
// unmeasured gap). results carry each case's Steps (connID→role) and Evidence.
// Only exercised edges that are also in the required set count toward the hit —
// exercisedEdges is intersected with required so out-of-band evidence (a type or
// role-pair not declared in the vocab) cannot inflate coverage.
func pathCoverage(results []agent.StepResult, required []project.VocabEdge, realRoles map[string]bool) contract.CoverageMeasurement {
	if len(required) == 0 {
		return contract.CoverageMeasurement{Known: false}
	}
	exercised, _ := exercisedEdges(results, required, realRoles)
	requiredKeys := make(map[string]bool, len(required))
	for _, e := range required {
		requiredKeys[edgeKey(e.FromRole, e.ToRole, e.Type)] = true
	}
	hit := 0
	for k := range exercised {
		if requiredKeys[k] {
			hit++
		}
	}
	return contract.CoverageMeasurement{
		Pct:   float64(hit) / float64(len(required)),
		Unit:  "path",
		Known: true,
	}
}

// sessionHasVocab reports whether any service declares a non-empty WS
// vocabulary (the SaaS/WS path-coverage surface). Structural, not mode-based —
// a session is routed to path coverage based on declared edges, not Mode.
func sessionHasVocab(sess *Session) bool {
	for _, svc := range sess.Config.Services {
		if svc.Vocabulary != nil && (len(svc.Vocabulary.Edges) > 0 || len(svc.Vocabulary.HTTPRoutes) > 0) {
			return true
		}
	}
	return false
}

// requiredEdges collects the declared required surface for path coverage:
// message_handled vocab edges (neither Unsupported nor Partial) PLUS one
// synthesized edge per declared http_trigger (an HTTP-triggered server push,
// modeled with an empty FromRole and Trigger="http_trigger"). Both are
// credited by the receive-driven exercisedEdges rule.
func requiredEdges(sess *Session) []project.VocabEdge {
	var out []project.VocabEdge
	for _, svc := range sess.Config.Services {
		if svc.Vocabulary != nil {
			for _, e := range svc.Vocabulary.Edges {
				if e.Trigger == "message_handled" && !e.Unsupported && !e.Partial {
					out = append(out, e)
				}
			}
		}
		// HTTP-triggered server-push edges: synthesize one required edge per
		// declared http_trigger so receive-driven attribution credits the push
		// when its recipient receives the message. Empty FromRole = system
		// origin; Trigger="http_trigger" distinguishes these from WS-relayed
		// vocab edges in gap output. (validateProtocolHTTPTriggers guarantees
		// ToRole/MessageType are non-empty and reference declared roles.)
		if svc.Protocol != nil {
			for _, tr := range svc.Protocol.HTTPTriggers {
				out = append(out, project.VocabEdge{
					FromRole: "",
					ToRole:   tr.Effect.ToRole,
					Type:     tr.Effect.MessageType,
					Trigger:  "http_trigger",
				})
			}
		}
		// Mounted HTTP routes (Hono-extracted): one required edge per
		// non-exempt route, same synthesis pattern as http_triggers. Any
		// response status credits exercise at attribution time — 4xx/5xx
		// still prove the route was reached; auth semantics belong to the
		// violations layer.
		if svc.Vocabulary != nil {
			for _, r := range svc.Vocabulary.HTTPRoutes {
				if r.Partial || r.Unsupported {
					continue
				}
				out = append(out, project.VocabEdge{
					FromRole: "http_client",
					ToRole:   "api",
					Type:     r.Method + " " + r.Path,
					Trigger:  "http_request",
				})
			}
		}
		// UI display promises: one required edge per declared assertion
		// (unsupported excluded), same synthesis pattern as HTTP routes.
		// FromRole "browser" / ToRole "web_ui" are reserved names no WS role
		// can collide with; Trigger "ui_assert" distinguishes them.
		if svc.Vocabulary != nil && svc.Vocabulary.UI != nil {
			for _, a := range svc.Vocabulary.UI.Assertions {
				if a.Unsupported {
					continue
				}
				out = append(out, project.VocabEdge{
					FromRole: "browser",
					ToRole:   "web_ui",
					Type:     "ui_assert " + a.ID,
					Trigger:  "ui_assert",
				})
			}
		}
	}
	return out
}

// realProcessRoles maps protocol role names backed by real-process actors
// (credential_ref names an actor with fidelity: real_process). Mirrors the
// scout helper of the same purpose; duplicated here so the session package
// stays scout-free.
func realProcessRoles(cfg *project.Config) map[string]bool {
	if cfg == nil {
		return nil
	}
	realActors := map[string]bool{}
	for _, a := range cfg.Actors {
		if a.Fidelity == project.FidelityRealProcess {
			realActors[a.Name] = true
		}
	}
	if len(realActors) == 0 {
		return nil
	}
	roles := map[string]bool{}
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		for name, r := range svc.Protocol.Roles {
			if r != nil && r.CredentialRef != "" && realActors[r.CredentialRef] {
				roles[name] = true
			}
		}
	}
	return roles
}

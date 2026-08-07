package session

import (
	"context"
	"os"
	"path/filepath"

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
	measurement := sess.lineCoverage(ctx)
	assessment, err := examinerHead.AssessCoverage(ctx, sess.Contract, results, measurement)
	if err == nil {
		sess.Assessment = assessment
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

// exercisedEdges computes which declared message edges a session's results
// exercised. Per case, connectionID→role is mapped from that case's ws_connect
// steps; a ws_send of type T from role Rs plus a matched ws_receive of T by role
// Rr exercises edge (Rs→Rr, T). Connections with no resolvable role are excluded
// (conservative). Returns the exercised set (keyed edgeKey) and the connRole map
// (for diagnostics). Pure; unit-testable without a live server.
func exercisedEdges(results []agent.StepResult, required []project.VocabEdge) (map[string]bool, map[string]string) {
	exercised := map[string]bool{}
	for _, r := range results {
		// connectionID → role for THIS case (roles are case-scoped via connect steps).
		connRole := map[string]string{}
		for _, s := range r.TestCase.Steps {
			if s.Action == "ws_connect" && s.Role != "" {
				connRole[s.ConnectionID] = s.Role
			}
		}
		sentByType := map[string]string{}      // type → sender role
		receivedByType := map[string]map[string]bool{} // type → set of recipient roles
		for _, ev := range r.Evidence {
			if ev.MatchedType == "" {
				continue
			}
			role := connRole[ev.ConnectionID]
			switch ev.Action {
			case "ws_send":
				if role != "" {
					sentByType[ev.MatchedType] = role
				}
			case "ws_receive":
				if !ev.ExpectAbsent && ev.Matched && role != "" {
					if receivedByType[ev.MatchedType] == nil {
						receivedByType[ev.MatchedType] = map[string]bool{}
					}
					receivedByType[ev.MatchedType][role] = true
				}
			}
		}
		for typ, sender := range sentByType {
			for recipient := range receivedByType[typ] {
				if recipient != sender {
					exercised[edgeKey(sender, recipient, typ)] = true
				}
			}
		}
	}
	return exercised, nil
}

// pathCoverage measures message-edge path coverage: exercised / required, over
// the session's declared vocab edges (message_handled, non-unsupported). Known is
// true whenever at least one required edge is declared (a measured 0%, not an
// unmeasured gap). results carry each case's Steps (connID→role) and Evidence.
func pathCoverage(results []agent.StepResult, required []project.VocabEdge) contract.CoverageMeasurement {
	if len(required) == 0 {
		return contract.CoverageMeasurement{Known: false}
	}
	exercised, _ := exercisedEdges(results, required)
	return contract.CoverageMeasurement{
		Pct:   float64(len(exercised)) / float64(len(required)),
		Unit:  "path",
		Known: true,
	}
}

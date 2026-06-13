package report

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/store"
)

// JUnit XML structures following the standard schema consumed by
// Jenkins, GitLab CI, GitHub Actions test-reporter, CircleCI, etc.

type junitXML struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string        `xml:"name,attr"`
	Tests    int           `xml:"tests,attr"`
	Failures int           `xml:"failures,attr"`
	Errors   int           `xml:"errors,attr"`
	Skipped  int           `xml:"skipped,attr"`
	Time     string        `xml:"time,attr,omitempty"`
	Cases    []junitCase   `xml:"testcase"`
}

type junitCase struct {
	Name      string          `xml:"name,attr"`
	Classname string          `xml:"classname,attr"`
	Time      string          `xml:"time,attr,omitempty"`
	Failure   *junitFailure   `xml:"failure,omitempty"`
	Error     *junitError     `xml:"error,omitempty"`
	Skip      *junitSkip      `xml:"skipped,omitempty"`
	SystemOut string          `xml:"system-out,omitempty"`
}

type junitFailure struct {
	Message  string `xml:"message,attr"`
	Type     string `xml:"type,attr,omitempty"`
	Contents string `xml:",chardata"`
}

type junitError struct {
	Message  string `xml:"message,attr"`
	Type     string `xml:"type,attr,omitempty"`
	Contents string `xml:",chardata"`
}

type junitSkip struct {
	Message string `xml:"message,attr,omitempty"`
}

// RenderJUnit produces a JUnit XML document from report data.
func RenderJUnit(data *ReportData) ([]byte, error) {
	suite := junitSuite{
		Name:  fmt.Sprintf("cerberus.%s", data.Session.ID),
		Tests: len(data.Verdicts),
	}

	if data.Summary != nil {
		suite.Time = fmt.Sprintf("%.3f", float64(data.Summary.DurationMs)/1000)
	}

	// Build trace lookup for duration info.
	traceDurations := make(map[int64]string) // trace_id → duration string
	for _, tr := range data.Traces {
		traceDurations[tr.ID] = "" // traces don't expose duration yet
	}

	for _, v := range data.Verdicts {
		tc := junitCase{
			Name:      verdictName(v),
			Classname: "cerberus",
		}

		// Build evidence summary for this verdict.
		evSummary := evidenceSummary(data.Evidence, v.TraceID)

		switch v.Status {
		case "pass":
			if evSummary != "" {
				tc.SystemOut = evSummary
			}
		case "fail":
			suite.Failures++
			contents := v.Reasoning
			if evSummary != "" {
				contents += "\n\n--- Evidence ---\n" + evSummary
			}
			tc.Failure = &junitFailure{
				Message:  fmt.Sprintf("FAIL: %s (confidence %.2f)", v.Target, v.Confidence),
				Type:     "AssertionError",
				Contents: truncate(contents, 2000),
			}
		case "uncertain":
			suite.Errors++
			contents := v.Reasoning
			if evSummary != "" {
				contents += "\n\n--- Evidence ---\n" + evSummary
			}
			tc.Error = &junitError{
				Message:  fmt.Sprintf("UNCERTAIN: %s (confidence %.2f)", v.Target, v.Confidence),
				Type:     "UncertainVerdict",
				Contents: truncate(contents, 2000),
			}
		case "skip":
			suite.Skipped++
			tc.Skip = &junitSkip{
				Message: truncate(v.Reasoning, 200),
			}
		}

		suite.Cases = append(suite.Cases, tc)
	}

	// If no verdicts, produce an empty suite with one "no results" case.
	if len(suite.Cases) == 0 {
		suite.Cases = append(suite.Cases, junitCase{
			Name:      "cerberus.no-results",
			Classname: "cerberus",
			Skip:      &junitSkip{Message: "no verdicts found for this session"},
		})
		suite.Tests = 1
		suite.Skipped = 1
	}

	doc := junitXML{
		Suites: []junitSuite{suite},
	}

	output, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal junit xml: %w", err)
	}

	return append([]byte(xml.Header), output...), nil
}

// evidenceSummary builds a compact text summary of evidence for a trace.
func evidenceSummary(evidence map[int64][]store.Evidence, traceID int64) string {
	evs, ok := evidence[traceID]
	if !ok || len(evs) == 0 {
		return ""
	}
	var b strings.Builder
	for i, ev := range evs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s", ev.Type, truncate(ev.Content, 200))
	}
	return b.String()
}

// verdictName builds a human-readable test case name from a verdict.
func verdictName(v store.Verdict) string {
	name := v.Target
	if name == "" {
		name = fmt.Sprintf("verdict-%d", v.ID)
	}
	return strings.NewReplacer(" ", "_", "/", ".", ":", "_").Replace(name)
}

// truncate shortens a string to maxLen characters with a truncation indicator.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "... (truncated)"
}

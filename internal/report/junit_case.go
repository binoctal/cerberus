package report

import (
	"fmt"

	"github.com/binoctal/cerberus/internal/store"
)

// buildJUnitCase constructs a JUnit test case from a verdict.
func buildJUnitCase(v store.Verdict, evidence map[int64][]store.Evidence) junitCase {
	tc := junitCase{
		Name:      verdictName(v),
		Classname: "cerberus",
	}

	evSummary := evidenceSummary(evidence, v.TraceID)

	switch v.Status {
	case "pass":
		if evSummary != "" {
			tc.SystemOut = evSummary
		}
	case "fail":
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
		tc.Skip = &junitSkip{
			Message: truncate(v.Reasoning, 200),
		}
	}

	return tc
}

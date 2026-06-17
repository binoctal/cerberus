package report

import (
	"fmt"
)

// buildJUnitSuite constructs a JUnit test suite from report data.
func buildJUnitSuite(data *ReportData) junitSuite {
	suite := junitSuite{
		Name:  fmt.Sprintf("cerberus.%s", data.Session.ID),
		Tests: len(data.Verdicts),
	}

	if data.Summary != nil {
		suite.Time = fmt.Sprintf("%.3f", float64(data.Summary.DurationMs)/1000)
	}

	// Build test cases from verdicts.
	for _, v := range data.Verdicts {
		tc := buildJUnitCase(v, data.Evidence)
		suite.Cases = append(suite.Cases, tc)

		// Update counters based on verdict status.
		switch v.Status {
		case "fail":
			suite.Failures++
		case "uncertain":
			suite.Errors++
		case "skip":
			suite.Skipped++
		}
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

	return suite
}

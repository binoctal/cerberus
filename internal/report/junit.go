package report

import (
	"encoding/xml"
	"fmt"
)

// RenderJUnit produces a JUnit XML document from report data.
func RenderJUnit(data *ReportData) ([]byte, error) {
	suite := buildJUnitSuite(data)

	doc := junitXML{
		Suites: []junitSuite{suite},
	}

	output, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal junit xml: %w", err)
	}

	return append([]byte(xml.Header), output...), nil
}

package report

import "encoding/xml"

// JUnit XML structures following the standard schema consumed by
// Jenkins, GitLab CI, GitHub Actions test-reporter, CircleCI, etc.

type junitXML struct {
	XMLName xml.Name     `xml:"testsuites"`
	Suites  []junitSuite `xml:"testsuite"`
}

type junitSuite struct {
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Errors   int         `xml:"errors,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Time     string      `xml:"time,attr,omitempty"`
	Cases    []junitCase `xml:"testcase"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr,omitempty"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Error     *junitError   `xml:"error,omitempty"`
	Skip      *junitSkip    `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
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

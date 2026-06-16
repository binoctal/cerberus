package report

import (
	"io"
	"strings"
)

// RenderHTML writes an HTML report to w.
func RenderHTML(w io.Writer, data *ReportData) error {
	return htmlTmpl.Execute(w, data)
}

// RenderHTMLString returns the HTML report as a string.
func RenderHTMLString(data *ReportData) (string, error) {
	var b strings.Builder
	if err := RenderHTML(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

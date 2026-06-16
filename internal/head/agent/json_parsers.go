package agent

import (
	"encoding/json"

	"github.com/binoctal/cerberus/internal/types"
)

// ruffResult maps the JSON structure from `ruff check --output-format json`.
type ruffResult struct {
	Filename string `json:"filename"`
	Line     int    `json:"line_no"`
	Rule     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

// parseRuffJSON converts ruff JSON output to CodeFindings.
func parseRuffJSON(stdout string) []types.CodeFinding {
	var results []ruffResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return nil
	}
	findings := make([]types.CodeFinding, 0, len(results))
	for _, r := range results {
		severity := "warning"
		if r.Severity == "error" || r.Severity == "fatal" {
			severity = "error"
		}
		findings = append(findings, types.CodeFinding{
			File:     r.Filename,
			Line:     r.Line,
			Rule:     r.Rule,
			Message:  r.Message,
			Severity: severity,
		})
	}
	return findings
}

// eslintResult maps the JSON structure from `eslint --format json`.
type eslintResult struct {
	FilePath     string          `json:"filePath"`
	Messages     []eslintMessage `json:"messages"`
	ErrorCount   int             `json:"errorCount"`
	WarningCount int             `json:"warningCount"`
}

type eslintMessage struct {
	RuleID  string `json:"ruleId"`
	Message string `json:"message"`
	Line    int    `json:"line"`
	Sev     int    `json:"severity"` // 0=off, 1=warn, 2=error
}

// parseESLintJSON converts eslint JSON output to CodeFindings.
func parseESLintJSON(stdout string) []types.CodeFinding {
	var results []eslintResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		return nil
	}
	var findings []types.CodeFinding
	for _, file := range results {
		for _, msg := range file.Messages {
			severity := "warning"
			if msg.Sev >= 2 {
				severity = "error"
			}
			findings = append(findings, types.CodeFinding{
				File:     file.FilePath,
				Line:     msg.Line,
				Rule:     msg.RuleID,
				Message:  msg.Message,
				Severity: severity,
			})
		}
	}
	return findings
}

package types

import (
	"encoding/json"
	"fmt"
	"time"
)

// ExecutorResult is the interface for all execution results.
type ExecutorResult interface {
	Success() bool
	Duration() time.Duration
	Summary() string
	Evidence() EvidenceData
}

// EvidenceData holds evidence collected from execution.
type EvidenceData struct {
	Type     string `json:"type"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}

// truncate limits s to maxRunes.
func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// --- HTTP Result ---

type HTTPResult struct {
	OK         bool              `json:"success"`
	StatusCode int               `json:"status_code"`
	Body       string            `json:"body"`
	Headers    map[string]string `json:"headers"`
	URL        string            `json:"url"`
	Latency    time.Duration     `json:"duration"`
	Err        string            `json:"error,omitempty"`
}

func (r HTTPResult) Success() bool            { return r.OK }
func (r HTTPResult) Duration() time.Duration  { return r.Latency }
func (r HTTPResult) Summary() string {
	return fmt.Sprintf("HTTP %d %s (%s)", r.StatusCode, r.URL, r.Latency)
}
func (r HTTPResult) Evidence() EvidenceData {
	return EvidenceData{Type: "http_response", Content: truncate(r.Body, 10000)}
}

// --- Process Result ---

type ProcessResult struct {
	OK       bool          `json:"success"`
	ExitCode int           `json:"exit_code"`
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

func (r ProcessResult) Success() bool            { return r.OK }
func (r ProcessResult) Duration() time.Duration  { return r.Latency }
func (r ProcessResult) Summary() string {
	return fmt.Sprintf("exit %d (%s)\nstdout: %s", r.ExitCode, r.Latency, truncate(r.Stdout, 500))
}
func (r ProcessResult) Evidence() EvidenceData {
	return EvidenceData{Type: "process_output", Content: truncate(r.Stdout, 10000)}
}

// --- File Result ---

type FileResult struct {
	OK      bool          `json:"success"`
	Path    string        `json:"path"`
	Content string        `json:"content,omitempty"`
	Exists  bool          `json:"exists,omitempty"`
	Matches []string      `json:"matches,omitempty"`
	Latency time.Duration `json:"duration"`
	Err     string        `json:"error,omitempty"`
}

func (r FileResult) Success() bool            { return r.OK }
func (r FileResult) Duration() time.Duration  { return r.Latency }
func (r FileResult) Summary() string {
	if r.Err != "" {
		return fmt.Sprintf("file %s: %s", r.Path, r.Err)
	}
	return fmt.Sprintf("file %s OK (%s)", r.Path, r.Latency)
}
func (r FileResult) Evidence() EvidenceData {
	return EvidenceData{Type: "file_content", Content: truncate(r.Content, 10000)}
}

// --- MCP Result ---

type MCPResult struct {
	OK      bool          `json:"success"`
	Body    string        `json:"body"`
	Latency time.Duration `json:"duration"`
	Err     string        `json:"error,omitempty"`
}

func (r MCPResult) Success() bool            { return r.OK }
func (r MCPResult) Duration() time.Duration  { return r.Latency }
func (r MCPResult) Summary() string {
	status := "error"
	if r.OK {
		status = "ok"
	}
	return fmt.Sprintf("MCP %s (%s)", status, r.Latency)
}
func (r MCPResult) Evidence() EvidenceData {
	return EvidenceData{Type: "mcp_response", Content: truncate(r.Body, 10000)}
}

// --- Code Result ---

type CodeResult struct {
	OK       bool          `json:"success"`
	Findings []CodeFinding `json:"findings"`
	Stats    CodeStats     `json:"stats"`
	Latency  time.Duration `json:"duration"`
	Err      string        `json:"error,omitempty"`
}

type CodeFinding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type CodeStats struct {
	FilesAnalyzed int     `json:"files_analyzed"`
	SymbolCount   int     `json:"symbol_count"`
	Coverage      float64 `json:"coverage,omitempty"`
}

func (r CodeResult) Success() bool            { return r.OK }
func (r CodeResult) Duration() time.Duration  { return r.Latency }
func (r CodeResult) Summary() string {
	return fmt.Sprintf("%d findings in %d files (%s)", len(r.Findings), r.Stats.FilesAnalyzed, r.Latency)
}
func (r CodeResult) Evidence() EvidenceData {
	content, _ := json.Marshal(r.Findings)
	return EvidenceData{Type: "code_findings", Content: string(content)}
}

// --- Wait Result ---

type WaitResult struct {
	OK      bool          `json:"success"`
	Latency time.Duration `json:"duration"`
}

func (r WaitResult) Success() bool            { return r.OK }
func (r WaitResult) Duration() time.Duration  { return r.Latency }
func (r WaitResult) Summary() string {
	return fmt.Sprintf("wait completed (%s)", r.Latency)
}
func (r WaitResult) Evidence() EvidenceData {
	return EvidenceData{Type: "wait", Content: "completed"}
}

// --- Browser Result ---

type BrowserResult struct {
	OK         bool          `json:"success"`
	URL        string        `json:"url"`
	Title      string        `json:"title"`
	Text       string        `json:"text,omitempty"`
	Screenshot string        `json:"screenshot,omitempty"` // base64 encoded
	EvalResult string        `json:"eval_result,omitempty"`
	Latency    time.Duration `json:"duration"`
	Err        string        `json:"error,omitempty"`
}

func (r BrowserResult) Success() bool            { return r.OK }
func (r BrowserResult) Duration() time.Duration  { return r.Latency }
func (r BrowserResult) Summary() string {
	status := "ok"
	if !r.OK {
		status = "error"
	}
	return fmt.Sprintf("browser %s %s (%s)", status, r.URL, r.Latency)
}
func (r BrowserResult) Evidence() EvidenceData {
	content := r.Text
	if r.EvalResult != "" {
		content = r.EvalResult
	}
	return EvidenceData{Type: "browser_content", Content: truncate(content, 10000)}
}

// --- Error Result (generic) ---

type ErrorResult struct {
	Err     string        `json:"error"`
	Latency time.Duration `json:"duration,omitempty"`
}

func (r ErrorResult) Success() bool            { return false }
func (r ErrorResult) Duration() time.Duration  { return r.Latency }
func (r ErrorResult) Summary() string          { return fmt.Sprintf("error: %s", r.Err) }
func (r ErrorResult) Evidence() EvidenceData {
	return EvidenceData{Type: "error", Content: r.Err}
}

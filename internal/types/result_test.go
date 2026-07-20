package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- truncate() tests ---

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxRunes int
		expected string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"truncated with ellipsis", "hello world", 8, "hello wo..."},
		{"empty string", "", 5, ""},
		{"maxRunes zero", "abc", 0, "..."},
		{"unicode runes", "你好世界测试", 3, "你好世..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, truncate(tt.input, tt.maxRunes))
		})
	}
}

// --- joinStrings() tests ---

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		sep      string
		expected string
	}{
		{"empty slice", []string{}, ", ", ""},
		{"single element", []string{"a"}, ", ", "a"},
		{"multiple elements", []string{"a", "b", "c"}, ", ", "a, b, c"},
		{"newline separator", []string{"x", "y"}, "\n", "x\ny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, joinStrings(tt.input, tt.sep))
		})
	}
}

// --- Summary() tests ---

func TestHTTPResult_Summary(t *testing.T) {
	r := HTTPResult{StatusCode: 200, URL: "http://x", Latency: 100 * time.Millisecond}
	assert.Contains(t, r.Summary(), "HTTP 200 http://x")
	assert.Contains(t, r.Summary(), "100ms")
}

func TestProcessResult_Summary(t *testing.T) {
	r := ProcessResult{ExitCode: 0, Latency: 50 * time.Millisecond, Stdout: "ok"}
	assert.Contains(t, r.Summary(), "exit 0")
	assert.Contains(t, r.Summary(), "ok")
}

func TestProcessResult_Summary_Truncates(t *testing.T) {
	longStdout := ""
	for i := 0; i < 1000; i++ {
		longStdout += "x"
	}
	r := ProcessResult{ExitCode: 1, Latency: time.Second, Stdout: longStdout}
	s := r.Summary()
	assert.Contains(t, s, "exit 1")
	// stdout in summary is truncated to 500 runes + "..."
	assert.Contains(t, s, "...")
}

func TestFileResult_Summary_Success(t *testing.T) {
	r := FileResult{Path: "/tmp/f", Latency: 10 * time.Millisecond}
	assert.Contains(t, r.Summary(), "file /tmp/f OK")
}

func TestFileResult_Summary_Error(t *testing.T) {
	r := FileResult{Path: "/tmp/f", Err: "not found"}
	assert.Contains(t, r.Summary(), "not found")
}

func TestMCPResult_Summary_OK(t *testing.T) {
	r := MCPResult{OK: true, Latency: 20 * time.Millisecond}
	assert.Contains(t, r.Summary(), "MCP ok")
}

func TestMCPResult_Summary_Error(t *testing.T) {
	r := MCPResult{OK: false, Latency: 20 * time.Millisecond}
	assert.Contains(t, r.Summary(), "MCP error")
}

func TestCodeResult_Summary(t *testing.T) {
	r := CodeResult{
		Findings: []CodeFinding{{File: "a.go"}, {File: "b.go"}},
		Stats:    CodeStats{FilesAnalyzed: 5},
		Latency:  200 * time.Millisecond,
	}
	assert.Contains(t, r.Summary(), "2 findings in 5 files")
}

func TestWaitResult_Summary(t *testing.T) {
	r := WaitResult{Latency: 3 * time.Second}
	assert.Contains(t, r.Summary(), "wait completed")
}

func TestBrowserResult_Summary_OK(t *testing.T) {
	r := BrowserResult{OK: true, URL: "http://x", Latency: 50 * time.Millisecond}
	assert.Contains(t, r.Summary(), "browser ok http://x")
}

func TestBrowserResult_Summary_Error(t *testing.T) {
	r := BrowserResult{OK: false, URL: "http://x", Latency: 50 * time.Millisecond}
	assert.Contains(t, r.Summary(), "browser error")
}

func TestErrorResult_Summary(t *testing.T) {
	r := ErrorResult{Err: "something went wrong"}
	assert.Equal(t, "error: something went wrong", r.Summary())
}

func TestDBResult_Summary(t *testing.T) {
	r := DBResult{OK: true, Driver: "sqlite", Rows: make([]map[string]any, 3), Latency: 10 * time.Millisecond}
	assert.Contains(t, r.Summary(), "db ok sqlite")
	assert.Contains(t, r.Summary(), "3 rows")
}

func TestGraphQLResult_Summary(t *testing.T) {
	r := GraphQLResult{OK: true, URL: "http://api/gql", Latency: 100 * time.Millisecond}
	assert.Contains(t, r.Summary(), "graphql ok http://api/gql")
}

func TestWSResult_Summary(t *testing.T) {
	r := WSResult{OK: true, URL: "ws://x", Messages: []string{"a", "b"}, Latency: 10 * time.Millisecond}
	assert.Contains(t, r.Summary(), "ws ok ws://x")
	// Summary reports matched/seen, not the legacy Messages list.
	assert.Contains(t, r.Summary(), "matched=0")
	assert.Contains(t, r.Summary(), "seen=0")
}

func TestWSResult_Summary_ReportsMatchedSeen(t *testing.T) {
	r := WSResult{
		OK:             true,
		URL:            "ws://x",
		MatchedMessage: `{"type":"pong"}`,
		SeenMessages:   []string{"tick", "tick"},
		Latency:        5 * time.Millisecond,
	}
	s := r.Summary()
	assert.Contains(t, s, "ws ok ws://x")
	assert.Contains(t, s, "matched=1")
	assert.Contains(t, s, "seen=2")
}

// --- Success() tests ---

func TestSuccess(t *testing.T) {
	assert.True(t, HTTPResult{OK: true}.Success())
	assert.False(t, HTTPResult{OK: false}.Success())
	assert.False(t, ErrorResult{Err: "x"}.Success())
	assert.True(t, WaitResult{OK: true}.Success())
}

// --- Duration() tests ---

func TestDuration(t *testing.T) {
	d := 42 * time.Millisecond
	assert.Equal(t, d, HTTPResult{Latency: d}.Duration())
	assert.Equal(t, d, ProcessResult{Latency: d}.Duration())
	assert.Equal(t, d, ErrorResult{Latency: d}.Duration())
}

// --- Evidence() tests ---

func TestHTTPResult_Evidence(t *testing.T) {
	r := HTTPResult{Body: "response body"}
	e := r.Evidence()
	assert.Equal(t, "http_response", e.Type)
	assert.Equal(t, "response body", e.Content)
}

func TestHTTPResult_Evidence_Truncates(t *testing.T) {
	longBody := ""
	for i := 0; i < 15000; i++ {
		longBody += "x"
	}
	r := HTTPResult{Body: longBody}
	e := r.Evidence()
	assert.True(t, len(e.Content) < len(longBody))
	assert.Contains(t, e.Content, "...")
}

func TestErrorResult_Evidence(t *testing.T) {
	r := ErrorResult{Err: "oops"}
	e := r.Evidence()
	assert.Equal(t, "error", e.Type)
	assert.Equal(t, "oops", e.Content)
}

func TestWSResult_Evidence_JoinMessages(t *testing.T) {
	r := WSResult{Messages: []string{"msg1", "msg2"}}
	e := r.Evidence()
	assert.Equal(t, "ws_messages", e.Type)
	assert.Contains(t, e.Content, "msg1")
	assert.Contains(t, e.Content, "msg2")
}

func TestBrowserResult_Evidence_PrefersEval(t *testing.T) {
	r := BrowserResult{Text: "page text", EvalResult: "42"}
	e := r.Evidence()
	assert.Equal(t, "42", e.Content)
}

func TestBrowserResult_Evidence_UsesText(t *testing.T) {
	r := BrowserResult{Text: "page text"}
	e := r.Evidence()
	assert.Equal(t, "page text", e.Content)
}

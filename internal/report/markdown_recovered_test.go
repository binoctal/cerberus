package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRenderSummaryTable_RecoveredRow(t *testing.T) {
	var b strings.Builder
	renderSummaryTable(&b, &session.SessionSummary{Failed: 1, Recovered: 1, TotalCases: 2})
	out := b.String()
	assert.Contains(t, out, "| **Recovered** | 1 |")
}

func TestStatusEmoji_Recovered(t *testing.T) {
	assert.Equal(t, "♻️ recovered", statusEmoji("recovered"))
}

func TestRenderVerdictsTable_RecoveredRow(t *testing.T) {
	var b strings.Builder
	renderVerdictsTable(&b, []store.Verdict{
		{Target: "ws://h/ws", Status: "pass", Recovered: true},
		{Target: "http://h/x", Status: "pass"},
	})
	out := b.String()
	assert.Contains(t, out, "♻️ recovered", "recovered verdict rendered with recovered emoji")
	assert.Contains(t, out, "✅ pass", "normal pass rendered normally")
}

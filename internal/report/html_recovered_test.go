package report

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

func TestRenderHTML_RecoveredSummaryCard(t *testing.T) {
	data := ReportData{
		Session:  &store.Session{ID: "s1"},
		Summary:  &session.SessionSummary{TotalCases: 2, Failed: 1, Recovered: 1},
		Verdicts: nil,
	}
	out, err := RenderHTMLString(&data)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, ">1</div><div class=\"label\">Recovered</div>"), "recovered summary card present")
	assert.True(t, strings.Contains(out, ".badge-recovered"), "badge-recovered CSS class defined")
}

func TestRenderHTML_RecoveredVerdictBadge(t *testing.T) {
	data := ReportData{
		Session: &store.Session{ID: "s1"},
		Summary: &session.SessionSummary{TotalCases: 1},
		Verdicts: []store.Verdict{
			{Target: "ws://h/ws", Status: "pass", Recovered: true},
		},
	}
	out, err := RenderHTMLString(&data)
	assert.NoError(t, err)
	assert.True(t, strings.Contains(out, "badge badge-recovered"), "rendered verdict badge uses class \"badge badge-recovered\"")
}

package dashboard

import (
	"context"
	"encoding/json"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// normalizeSelection ensures selected index is within valid range
func normalizeSelection(selected int, sessionCount int) int {
	sel := selected
	if sel >= sessionCount {
		sel = sessionCount - 1
	}
	if sel < 0 {
		sel = 0
	}
	return sel
}

// loadSessionData loads verdicts, traces, and summary for a session
func loadSessionData(storeRef store.Store, sess store.Session) ([]store.Verdict, []store.Trace, *session.SessionSummary) {
	ctx := context.Background()
	id := sess.ID

	verdicts, _ := storeRef.GetVerdicts(ctx, id)
	traces, _ := storeRef.GetTraces(ctx, id)

	if verdicts == nil {
		verdicts = []store.Verdict{}
	}
	if traces == nil {
		traces = []store.Trace{}
	}

	summary := parseSessionSummary(sess.Stats)
	return verdicts, traces, summary
}

// parseSessionSummary parses session stats into a summary
func parseSessionSummary(stats string) *session.SessionSummary {
	if stats == "" || stats == "{}" {
		return nil
	}

	var s session.SessionSummary
	if err := json.Unmarshal([]byte(stats), &s); err == nil {
		return &s
	}
	return nil
}

package dashboard

import (
	"time"

	"github.com/binoctal/cerberus/internal/session"
	"github.com/binoctal/cerberus/internal/store"
)

// tickMsg triggers a periodic data refresh.
type tickMsg time.Time

// refreshMsg carries refreshed data from the store.
type refreshMsg struct {
	sessions []store.Session
	selected int
	verdicts []store.Verdict
	traces   []store.Trace
	summary  *session.SessionSummary
}

// Model is the bubbletea model for the dashboard.
type Model struct {
	store    *store.Store
	sessions []store.Session
	selected int
	verdicts []store.Verdict
	traces   []store.Trace
	summary  *session.SessionSummary
	width    int
	height   int
	detail   bool // whether detail view is shown
	quitting bool
}

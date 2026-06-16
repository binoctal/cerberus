package server

import (
	"fmt"
	"net/http"

	"github.com/binoctal/cerberus/internal/report"
)

// handleGetReport returns a test report in various formats.
func (srv *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	data, err := report.BuildReport(r.Context(), srv.store, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Content negotiation: text/markdown, text/html, text/plain, JSON default.
	accept := r.Header.Get("Accept")
	switch accept {
	case "text/plain":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "Session: %s\n", data.Session.ID)
		_, _ = fmt.Fprintf(w, "Goal: %s\n", data.Session.Goal)
		_, _ = fmt.Fprintf(w, "Status: %s\n", data.Session.Status)
		_, _ = fmt.Fprintf(w, "Started: %s\n", data.Session.StartedAt)
		if data.Session.FinishedAt != "" {
			_, _ = fmt.Fprintf(w, "Finished: %s\n", data.Session.FinishedAt)
		}
		_, _ = fmt.Fprintf(w, "Traces: %d\n", len(data.Traces))
		_, _ = fmt.Fprintf(w, "Verdicts: %d\n", len(data.Verdicts))
		if data.Session.Stats != "" && data.Session.Stats != "{}" {
			_, _ = fmt.Fprintf(w, "Stats: %s\n", data.Session.Stats)
		}
	case "text/markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = w.Write([]byte(report.RenderMarkdown(data)))
	case "text/html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = report.RenderHTML(w, data)
	case "application/junit+xml", "application/xml":
		xmlBytes, err := report.RenderJUnit(data)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "render JUnit: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/junit+xml; charset=utf-8")
		_, _ = w.Write(xmlBytes)
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"session":  data.Session,
			"traces":   data.Traces,
			"verdicts": data.Verdicts,
		})
	}
}

package agent

import (
	"encoding/json"

	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCaptureFromHTTPBody_EmptyListSignal pins the empty-list distinction the
// runSteps skip decision needs: indexing into an EMPTY array is the
// live-correct-chain-but-nothing-to-chain case; every other capture miss
// (missing key, out-of-range on a NON-empty array, non-scalar leaf) stays a
// hard error.
func TestCaptureFromHTTPBody_EmptyListSignal(t *testing.T) {
	_, err := captureFromHTTPBody(`{"reports":[]}`, map[string]string{"reports.0.id": "p"})
	require.ErrorIs(t, err, ErrEmptyListCapture, "indexing an empty wrapped list must carry the empty-list signal")

	_, err = captureFromHTTPBody(`[]`, map[string]string{"0.id": "p"})
	require.ErrorIs(t, err, ErrEmptyListCapture, "indexing an empty top-level array must carry the empty-list signal")

	_, err = captureFromHTTPBody(`{"reports":[{"id":"a"}]}`, map[string]string{"reports.1.id": "p"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrEmptyListCapture, "out-of-range on a non-empty array is a shape/generator error")

	_, err = captureFromHTTPBody(`{"other":1}`, map[string]string{"reports.0.id": "p"})
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrEmptyListCapture, "a missing key is shape drift, not an empty list")

	// length: on an empty array is a valid capture (0), never a skip.
	captured, err := captureFromHTTPBody(`{"reports":[]}`, map[string]string{"length:reports": "n"})
	require.NoError(t, err)
	require.Equal(t, map[string]string{"n": "0"}, captured)
}

// TestCaptureEmptyListSkips pins run30's biggest honest-failure family: a
// chained param capture (X.0.id) against a list route that currently returns
// an EMPTY list is a live-correct chain with nothing to chain from — the case
// must end as a skip, not a failure, and the target step must not run.
func TestCaptureEmptyListSkips(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"things": []any{}})
	}))
	t.Cleanup(srv.Close)

	tc := &TestCase{
		ID:     "tc-capture-empty-list",
		Target: srv.URL,
		Steps: []TestStep{
			{Action: "http_request", URL: srv.URL + "/api/things", Method: "GET",
				ExpectStatusClass: "2xx",
				Capture:           map[string]string{"things.0.id": "p_id"}},
			{Action: "http_request", URL: srv.URL + "/api/things/{{case.p_id}}", Method: "DELETE",
				ExpectStatusClass: "2xx_4xx"},
		},
	}
	se, _ := newStepExecutionObs(t, tc)
	res := se.runSteps()

	require.Equal(t, StepSkipped, res.Status, "empty-list capture must skip, not fail")
	require.Equal(t, int32(1), hits.Load(), "the target step must not run after an empty-list capture")
}

// TestCaptureMissingPathStillFails: a genuinely missing capture path (shape
// drift, not an empty list) remains a hard failure — clear failure over a
// silently-wrong later request.
func TestCaptureMissingPathStillFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"things": []map[string]string{{"id": "a"}}})
	}))
	t.Cleanup(srv.Close)

	tc := &TestCase{
		ID:     "tc-capture-missing-path",
		Target: srv.URL,
		Steps: []TestStep{
			{Action: "http_request", URL: srv.URL + "/api/things", Method: "GET",
				ExpectStatusClass: "2xx",
				Capture:           map[string]string{"wrong.0.id": "p_id"}},
		},
	}
	se, _ := newStepExecutionObs(t, tc)
	res := se.runSteps()

	require.Equal(t, StepFailed, res.Status, "missing capture path stays a hard failure")
	require.Error(t, res.Error, "failure carries the capture error")
}

package agent

import (
	"strings"
	"testing"
)

func TestCaptureFromHTTPBody(t *testing.T) {
	got, err := captureFromHTTPBody(`{"plan":{"id":"plan_123"},"n":7}`,
		map[string]string{"plan.id": "planId", "n": "count"})
	if err != nil {
		t.Fatal(err)
	}
	if got["planId"] != "plan_123" || got["count"] != "7" {
		t.Fatalf("got %v", got)
	}
	if _, err := captureFromHTTPBody(`{}`, map[string]string{"plan.id": "planId"}); err == nil {
		t.Fatal("missing path must be a hard error")
	}
	if _, err := captureFromHTTPBody(`not json`, map[string]string{"a": "a"}); err == nil {
		t.Fatal("unparseable body must be a hard error")
	}
}

// TestCaptureFromHTTPBody_ArrayIndexing: a numeric dot-path segment indexes
// an array node — the param-chain pick shapes the HTTP route generator emits
// ("0.id" for a top-level array, "devices.0.id" for the wrapped lists the
// open-agents routes actually return).
func TestCaptureFromHTTPBody_ArrayIndexing(t *testing.T) {
	// Top-level array of records.
	got, err := captureFromHTTPBody(`[{"id":"d_1"},{"id":"d_2"}]`,
		map[string]string{"0.id": "first"})
	if err != nil {
		t.Fatal(err)
	}
	if got["first"] != "d_1" {
		t.Fatalf("got %v", got)
	}
	// Wrapped list: array nested under an object key.
	got, err = captureFromHTTPBody(`{"devices":[{"id":"dev_9","name":"x"},{"id":"dev_2"}],"total":2}`,
		map[string]string{"devices.0.id": "deviceId", "devices.1.id": "second"})
	if err != nil {
		t.Fatal(err)
	}
	if got["deviceId"] != "dev_9" {
		t.Fatalf("got %v", got)
	}
	// Out-of-range index against a live array is the not-found hard error.
	if _, err := captureFromHTTPBody(`{"devices":[]}`,
		map[string]string{"devices.0.id": "deviceId"}); err == nil {
		t.Fatal("out-of-range index must be a hard error")
	}
	// A numeric segment against a non-array, non-map node is not found.
	if _, err := captureFromHTTPBody(`{"devices":7}`,
		map[string]string{"devices.0.id": "deviceId"}); err == nil {
		t.Fatal("numeric segment against a scalar must be a hard error")
	}
	// length: composes with array indexing.
	got, err = captureFromHTTPBody(`{"teams":[{"id":"t1"}]}`,
		map[string]string{"length:teams": "teamCount"})
	if err != nil {
		t.Fatal(err)
	}
	if got["teamCount"] != "1" {
		t.Fatalf("got %v", got)
	}
}

func TestSubstituteCaseParams(t *testing.T) {
	s := TestStep{URL: "http://x/api/users/{{case.planId}}", Body: `{"plan":"{{case.planId}}"}`, Message: `{"id":"{{case.planId}}"}`}
	out := substituteCaseParams(s, map[string]string{"planId": "plan_9"})
	if !strings.Contains(out.URL, "plan_9") || !strings.Contains(out.Body, "plan_9") || !strings.Contains(out.Message, "plan_9") {
		t.Fatalf("unsubstituted: %+v", out)
	}
}

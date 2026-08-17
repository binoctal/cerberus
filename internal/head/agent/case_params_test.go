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

func TestSubstituteCaseParams(t *testing.T) {
	s := TestStep{URL: "http://x/api/users/{{case.planId}}", Body: `{"plan":"{{case.planId}}"}`, Message: `{"id":"{{case.planId}}"}`}
	out := substituteCaseParams(s, map[string]string{"planId": "plan_9"})
	if !strings.Contains(out.URL, "plan_9") || !strings.Contains(out.Body, "plan_9") || !strings.Contains(out.Message, "plan_9") {
		t.Fatalf("unsubstituted: %+v", out)
	}
}

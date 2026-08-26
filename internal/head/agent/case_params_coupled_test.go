package agent

import (
	"testing"
)

// Protocol-coupled UI assertions (vocab from_api): the http step captures a
// value that the browser_expect selector must consume as {{case.<name>}}.
// substituteCaseParams previously only rewrote URL/Body/Message — browser
// steps carry their selector in Target.
func TestSubstituteCaseParamsTarget(t *testing.T) {
	s := TestStep{Action: "browser_expect",
		Target:  "text={{case.onlineCount}} devices online",
		URL:     "/dashboard/missions",
		Body:    `{"id":"{{case.onlineCount}}"}`,
		Message: "{{case.missionId}} done",
	}
	got := substituteCaseParams(s, map[string]string{"onlineCount": "3", "missionId": "job_x"})
	if got.Target != "text=3 devices online" {
		t.Fatalf("Target not substituted: %q", got.Target)
	}
	if got.URL != "/dashboard/missions" {
		t.Fatalf("URL must pass through untouched: %q", got.URL)
	}
	if got.Body != `{"id":"3"}` || got.Message != "job_x done" {
		t.Fatalf("existing substitutions regressed: %+v", got)
	}

	// Unknown names stay verbatim (same policy as URL/Body).
	kept := substituteCaseParams(TestStep{Target: "text={{case.missing}}"}, nil)
	if kept.Target != "text={{case.missing}}" {
		t.Fatalf("unknown placeholder must stay verbatim, got %q", kept.Target)
	}
}

func TestCaptureFromHTTPBodyLength(t *testing.T) {
	body := `{"devices":[{"id":"a"},{"id":"b"},{"id":"c"}],"total":3}`
	out, err := captureFromHTTPBody(body, map[string]string{
		"length:devices": "onlineCount",
		"total":          "rawTotal",
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if out["onlineCount"] != "3" {
		t.Fatalf("length capture = %q, want 3", out["onlineCount"])
	}
	if out["rawTotal"] != "3" {
		t.Fatalf("scalar capture regressed: %v", out)
	}

	if _, err := captureFromHTTPBody(`{"devices":{"a":1}}`, map[string]string{"length:devices": "n"}); err == nil {
		t.Fatal("length: on a non-array must be an error, not a silent value")
	}
	if _, err := captureFromHTTPBody(`{"x":1}`, map[string]string{"length:missing": "n"}); err == nil {
		t.Fatal("length: on a missing path must be an error")
	}
}

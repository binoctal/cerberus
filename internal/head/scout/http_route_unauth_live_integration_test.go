//go:build integration

// Live gate for the v2 unauth tier: every -unauth case generated from the
// dogfood open-agents vocab must actually be rejected (4xx) by the real
// wrangler dev server with a bare client. This is the vacuity guard the
// Task 8 break test depends on — zero unauth cases here means the auth
// facts stopped flowing and the generator went vacuous.
package scout

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestHTTPRouteUnauthCasesLive(t *testing.T) {
	vocabPath := filepath.Join("..", "..", "..", "dogfood", "realtime-e2e", ".cerberus", "vocab", "open-agents.vocab.yaml")
	if _, err := os.Stat(vocabPath); err != nil {
		t.Skipf("dogfood vocab not available: %v", err)
	}
	vocab, err := project.LoadVocabulary(vocabPath)
	if err != nil {
		t.Fatalf("load dogfood vocab: %v", err)
	}
	const base = "http://localhost:8989"
	client := &http.Client{Timeout: 15 * time.Second}
	// Skip-if-server-down convention (same as the agent package's live tests).
	probe, err := client.Get(base + "/health")
	if err != nil {
		t.Skipf("open-agents dev server not reachable on %s: %v", base, err)
	}
	probe.Body.Close()

	svc := project.Service{Name: "open-agents", URL: base, Vocabulary: vocab}
	cases := httpRouteCases(svc)
	var unauth []agent.TestCase
	for _, c := range cases {
		if strings.HasSuffix(c.ID, "-unauth") {
			unauth = append(unauth, c)
		}
	}
	// Pinned from first observation (2026-08-26): 298 unauth cases — 343
	// vocab routes, 298 carrying the use:/api/* JWT gate (auth required),
	// 43 public skip-list routes (auth none) and 2 root routes outside the
	// /api glob (unknown) emit no unauth case. Zero unauth cases = FAIL:
	// that is the vacuity the break test needs to be able to detect.
	if len(unauth) == 0 {
		t.Fatal("zero -unauth cases generated — auth facts are not flowing (vacuous gate)")
	}
	if len(unauth) < 50 {
		t.Fatalf("only %d -unauth cases, expected >= 50 (observed 298 on first pin)", len(unauth))
	}

	rejected, failed := 0, 0
	for _, c := range unauth {
		step := c.Steps[0]
		var body *strings.Reader
		if step.Body != "" {
			body = strings.NewReader(step.Body)
		}
		var req *http.Request
		if body != nil {
			req, err = http.NewRequest(step.Method, step.URL, body)
		} else {
			req, err = http.NewRequest(step.Method, step.URL, nil)
		}
		if err != nil {
			t.Errorf("%s: build request: %v", c.ID, err)
			continue
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Errorf("%s (%s %s): request failed: %v", c.ID, step.Method, step.URL, err)
			failed++
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode <= 499 {
			rejected++
			continue
		}
		t.Errorf("%s (%s %s): status %d, want 4xx rejection", c.ID, step.Method, step.URL, resp.StatusCode)
		failed++
	}
	fmt.Printf("unauth gate: %d/%d rejected (4xx), %d failures\n", rejected, len(unauth), failed)
	if failed > 0 {
		t.Fatalf("%d of %d unauth cases were not rejected with 4xx", failed, len(unauth))
	}
}

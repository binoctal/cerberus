//go:build integration

// Proves the v2 extractors recover real facts from the actual open-agents
// worker source (not fixtures). Sibling checkout required (../open-agents),
// same convention as the UI-titles integration test.
package vocabextract

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestExtractHTTPFactsAgainstRealOpenAgents(t *testing.T) {
	// Pinned worker entry (the one recorded in the dogfood vocab source
	// files): apps/api, not the web-worker path sketched in the brief.
	worker := "../../../open-agents/apps/api/src/worker.ts"
	if _, err := os.Stat(worker); err != nil {
		t.Skipf("sibling open-agents checkout not available: %v", err)
	}
	raw, err := Extract(context.Background(), worker)
	if err != nil {
		t.Skipf("node unavailable or extraction failed: %v", err)
	}
	var out struct {
		HTTPRoutes []struct {
			Method      string         `json:"method"`
			Path        string         `json:"path"`
			Middlewares []string       `json:"middlewares"`
			MinBody     map[string]any `json:"min_body"`
		} `json:"http_routes"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	authed, withBody, withAnyMw := 0, 0, 0
	for _, r := range out.HTTPRoutes {
		if len(r.Middlewares) > 0 {
			withAnyMw++
		}
		for _, mw := range r.Middlewares {
			if strings.Contains(strings.ToLower(mw), "auth") {
				authed++
				break
			}
		}
		if len(r.MinBody) > 0 {
			withBody++
		}
	}
	t.Logf("observed: %d routes, %d with middleware, %d with auth middleware, %d with min_body",
		len(out.HTTPRoutes), withAnyMw, authed, withBody)

	// Thresholds re-pinned 2026-08-26 after the glob-prefix + anonymous-use
	// hardening (observed: 343 routes, 343 with a middleware = 100%; floor
	// pinned at >= 60%). Before the hardening only 1 route carried a
	// middleware. Loosen ONLY with a written justification referencing fresh
	// observed counts.
	//
	// The auth-name heuristic counter stays a logged observation, NOT a gate:
	// the real open-agents auth is an ANONYMOUS app.use('/api/*', async ...)
	// captured as "use:/api/*", which contains no auth|bearer|jwt substring.
	// Auth facts for this SUT flow through the hand-curated
	// http_auth_middlewares list intersected with per-route middlewares
	// (spec §1), never through the name regex — a name-based floor would
	// forever read 0 here while the auth tier is fully populated.
	//
	// min_body stays a logged observation too: the repo has no
	// zValidator('json', zod) route schemas at all, so 0 is the
	// omit-not-guess contract working as designed.
	if len(out.HTTPRoutes) < 300 {
		t.Fatalf("expected 300+ routes (dogfood vocab has 343), got %d", len(out.HTTPRoutes))
	}
	if withAnyMw < len(out.HTTPRoutes)*6/10 {
		t.Fatalf("middleware facts suspiciously low: %d of %d routes carry a middleware (observed 343/343 = 100%%)",
			withAnyMw, len(out.HTTPRoutes))
	}
}

//go:build integration

// Proves the extractor recovers real candidates from the actual open-agents
// source (not a hand-crafted fixture) — the point of this pass is grounded
// extraction, so the proof has to run against the real repo. Sibling
// checkout required (../open-agents), same convention as the browser-leg
// integration test.
package vocabextract

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractUITitlesAgainstRealOpenAgents(t *testing.T) {
	webRoot := "../../../open-agents/apps/web/src"
	appTSX, err := os.ReadFile(filepath.Join(webRoot, "App.tsx"))
	if err != nil {
		t.Skipf("sibling open-agents checkout not available: %v", err)
	}
	locale, err := os.ReadFile(filepath.Join(webRoot, "i18n/locales/en.json"))
	if err != nil {
		t.Fatalf("read en.json: %v", err)
	}
	routes := ExtractDashboardRoutes(string(appTSX))
	if len(routes) < 15 {
		t.Fatalf("expected the dashboard route block to yield 15+ routes, got %d: %+v", len(routes), routes)
	}

	pagesDir := filepath.Join(webRoot, "pages")
	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		t.Fatal(err)
	}

	var candidates []UITitleCandidate
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tsx" {
			continue
		}
		comp := e.Name()[:len(e.Name())-len(".tsx")]
		route, mounted := routes[comp]
		if !mounted {
			continue // component not on a /dashboard route (admin/landing/etc.) — out of scope
		}
		src, err := os.ReadFile(filepath.Join(pagesDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range ExtractPageHeaderTitles(string(src)) {
			text, err := ResolveLocale(locale, key)
			if err != nil {
				t.Errorf("%s: %v", comp, err)
				continue
			}
			candidates = append(candidates, UITitleCandidate{
				Route: route, Component: comp, I18nKey: key, Text: text, SourceFile: e.Name(),
			})
		}
	}

	// Known-present candidates, hand-verified 2026-08-26 against the same
	// source (agents-page-title / logs-page-title were authored by reading
	// exactly these files). This is the "did automation recover what a
	// human found by hand" proof.
	want := map[string]string{
		"/dashboard/agents": "Custom Agents",
		"/dashboard/logs":   "Logs Center",
	}
	found := map[string]string{}
	for _, c := range candidates {
		found[c.Route] = c.Text
	}
	for route, text := range want {
		if found[route] != text {
			t.Errorf("route %s: got %q, want %q (all candidates: %+v)", route, found[route], text, candidates)
		}
	}

	// The collision guard: resolve every sidebar nav label and filter
	// candidates whose text is identical to one. SearchPage's <PageHeader
	// title={t('nav.search')}> is the real, currently-present case — its
	// text resolves to "Search", the exact sidebar label — proving the
	// guard is load-bearing, not decorative.
	navSrc, err := os.ReadFile(filepath.Join(webRoot, "components/layout/DashboardLayout.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	var navLabels []string
	for _, key := range ExtractNavLabelKeys(string(navSrc)) {
		text, err := ResolveLocale(locale, key)
		if err != nil {
			continue // nav array can carry non-i18n-key literals; skip unresolvable
		}
		navLabels = append(navLabels, text)
	}

	var safe, flagged []UITitleCandidate
	for _, c := range candidates {
		if CollidesWithNav(c.Text, navLabels) {
			flagged = append(flagged, c)
		} else {
			safe = append(safe, c)
		}
	}
	foundSearchCollision := false
	for _, c := range flagged {
		if c.Component == "SearchPage" {
			foundSearchCollision = true
		}
	}
	if !foundSearchCollision {
		t.Errorf("expected SearchPage's title={t('nav.search')} to be flagged as a nav collision; flagged=%+v", flagged)
	}
	for _, c := range safe {
		if c.Component == "AgentsPage" || c.Component == "LogsPage" {
			continue // expected safe
		}
	}
	t.Logf("recovered %d candidates: %d safe, %d flagged as nav collisions\nsafe: %+v\nflagged: %+v",
		len(candidates), len(safe), len(flagged), safe, flagged)
}

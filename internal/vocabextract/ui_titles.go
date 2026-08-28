package vocabextract

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// UITitleCandidate is a proposed static display-promise assertion, mined
// from source rather than guessed by an LLM (the project already hit the
// LLM-invents-plausible-but-absent-surface failure mode for HTTP endpoints —
// see downgradeUnmodeledHTTPProbes — so UI vocab generation follows the same
// grounded-extraction discipline as the HTTP/WS passes: real regex matches
// against real source, not model knowledge).
type UITitleCandidate struct {
	Route      string // e.g. "/dashboard/agents"
	Component  string // React component name, for traceability
	I18nKey    string // e.g. "agent.title"
	Text       string // resolved locale string, e.g. "Custom Agents"
	SourceFile string // page file the title was found in
}

var (
	routeChildRe  = regexp.MustCompile(`<Route\s+path="([^"]+)"\s+element=\{<(\w+)\b`)
	routeIndexRe  = regexp.MustCompile(`<Route\s+index\s+element=\{<(\w+)\b`)
	pageHeaderRe  = regexp.MustCompile(`<PageHeader\b`)
	titlePropRe   = regexp.MustCompile(`title=\{t\('([^']+)'\)\}`)
	navLabelKeyRe = regexp.MustCompile(`labelKey:\s*"([^"]+)"`)
	pageHeaderWin = 6 // lines to scan after <PageHeader for its title prop
)

// literalTCallRe matches a hardcoded t('key') call anywhere in a source
// file — the layout's persistent chrome (search icon, notifications button,
// etc.) renders its own labels this way rather than through the NAV_ITEMS
// array, so a collision source scanning only labelKey: entries misses them
// (found live: DashboardLayout.tsx's search icon is {t('layout.search')},
// NOT part of the array, and resolves to the same "Search" text as the
// array-driven nav.search key — see the integration test).
var literalTCallRe = regexp.MustCompile(`\bt\('([^']+)'\)`)

// ExtractNavLabelKeys pulls every i18n key referenced in the persistent
// layout chrome source (DashboardLayout.tsx): both `labelKey: "nav.foo"`
// entries (the sidebar nav array, rendered via a dynamic t(item.labelKey)
// so a literal-call scan alone would miss them) and any hardcoded `t('...')`
// call elsewhere in the same file (icons/buttons outside the array).
// Resolving the union against the locale file and feeding it to
// CollidesWithNav is what makes a proposed title candidate safe to trust.
func ExtractNavLabelKeys(navSrc string) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(k string) {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, m := range navLabelKeyRe.FindAllStringSubmatch(navSrc, -1) {
		add(m[1])
	}
	for _, m := range literalTCallRe.FindAllStringSubmatch(navSrc, -1) {
		add(m[1])
	}
	return keys
}

// ExtractDashboardRoutes maps component name -> mounted route path, scoped to
// the single `<Route path="/dashboard" element={...}>` block's direct
// children (the shape this project's router actually uses: one parent route,
// flat relative-path children, plus one index route). Deeper multi-level
// nesting is NOT walked — a component outside this block is simply absent
// from the returned map, which the caller must treat as "route unknown,
// skip" rather than guessing.
func ExtractDashboardRoutes(routerSrc string) map[string]string {
	start := strings.Index(routerSrc, `<Route path="/dashboard" element={`)
	if start < 0 {
		return map[string]string{}
	}
	// Scope to the block: from the parent Route to its closing `</Route>`
	// (the first one at the same nesting — approximated by the next
	// occurrence, which holds for this project's flat single-level shape).
	end := strings.Index(routerSrc[start:], "</Route>")
	block := routerSrc[start:]
	if end >= 0 {
		block = routerSrc[start : start+end]
	}

	out := map[string]string{}
	for _, m := range routeIndexRe.FindAllStringSubmatch(block, -1) {
		out[m[1]] = "/dashboard"
	}
	for _, m := range routeChildRe.FindAllStringSubmatch(block, -1) {
		path, comp := m[1], m[2]
		out[comp] = "/dashboard/" + strings.TrimPrefix(path, "/")
	}
	return out
}

// ExtractPageHeaderTitles scans one page source file for `<PageHeader ...
// title={t('key')} ...>` usages and returns their i18n keys, deduplicated in
// first-seen order — a page rendering the same header in two layout branches
// (PromptLabPage does) yields ONE key, not a duplicate assertion (unresolved —
// ResolveLocale does the JSON lookup, kept separate so callers can batch
// multiple pages against one parsed locale file).
func ExtractPageHeaderTitles(src string) []string {
	lines := strings.Split(src, "\n")
	seen := map[string]bool{}
	var keys []string
	for i, line := range lines {
		if !pageHeaderRe.MatchString(line) {
			continue
		}
		end := i + pageHeaderWin
		if end > len(lines) {
			end = len(lines)
		}
		window := strings.Join(lines[i:end], "\n")
		if m := titlePropRe.FindStringSubmatch(window); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			keys = append(keys, m[1])
		}
	}
	return keys
}

// ResolveLocale resolves a dotted i18n key (e.g. "agent.title") against a
// parsed locale JSON file (e.g. en.json), returning the leaf string.
func ResolveLocale(localeJSON []byte, key string) (string, error) {
	var root map[string]any
	if err := json.Unmarshal(localeJSON, &root); err != nil {
		return "", fmt.Errorf("ui-vocab: parse locale JSON: %w", err)
	}
	var cur any = root
	for _, seg := range strings.Split(key, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", fmt.Errorf("ui-vocab: key %q not found (stopped at %q)", key, seg)
		}
		cur, ok = m[seg]
		if !ok {
			return "", fmt.Errorf("ui-vocab: key %q not found (missing %q)", key, seg)
		}
	}
	s, ok := cur.(string)
	if !ok {
		return "", fmt.Errorf("ui-vocab: key %q does not resolve to a string", key)
	}
	return s, nil
}

// CollidesWithNav reports whether text exactly matches one of the persistent
// sidebar nav labels — an assertion on such text would pass on EVERY
// dashboard page (the sidebar renders everywhere) and prove nothing about
// the specific page's own content. This is the check a human author must
// otherwise do by hand (as done for the missions/devices/agents/logs
// assertions authored 2026-08-26).
func CollidesWithNav(text string, navLabels []string) bool {
	for _, n := range navLabels {
		if n == text {
			return true
		}
	}
	return false
}

// UITitleResult separates recovered candidates into Safe (unique to the
// page, ready to paste into vocab.yaml as a static assertion) and Flagged
// (text identical to a persistent-chrome label — a real collision risk, not
// a false alarm; see CollidesWithNav). It never writes anything — vocab.yaml
// carries hand-curated entries (protocol-coupled assertions, non-PageHeader
// pages) that a blind overwrite would clobber, so committing candidates
// stays a human step, same discipline as the WS/HTTP extractor's dry-run.
type UITitleResult struct {
	Safe    []UITitleCandidate
	Flagged []UITitleCandidate
}

// ExtractUITitleCandidatesFromDisk runs the full pipeline: parse the router
// source for the /dashboard route table, scan every .tsx file in pagesDir
// for PageHeader titles, resolve every key against locale, and split the
// result by collision with the persistent layout chrome (navSrc). Pages
// whose component isn't mounted under /dashboard (routes map miss) are
// silently skipped — this pass only proposes assertions for routes it can
// name with certainty, never a guess.
func ExtractUITitleCandidatesFromDisk(routerFile, pagesDir, navFile, localeFile string) (UITitleResult, error) {
	router, err := os.ReadFile(routerFile)
	if err != nil {
		return UITitleResult{}, fmt.Errorf("ui-vocab: read router file: %w", err)
	}
	nav, err := os.ReadFile(navFile)
	if err != nil {
		return UITitleResult{}, fmt.Errorf("ui-vocab: read nav file: %w", err)
	}
	locale, err := os.ReadFile(localeFile)
	if err != nil {
		return UITitleResult{}, fmt.Errorf("ui-vocab: read locale file: %w", err)
	}
	routes := ExtractDashboardRoutes(string(router))

	entries, err := os.ReadDir(pagesDir)
	if err != nil {
		return UITitleResult{}, fmt.Errorf("ui-vocab: read pages dir: %w", err)
	}
	var navLabels []string
	for _, key := range ExtractNavLabelKeys(string(nav)) {
		if text, rerr := ResolveLocale(locale, key); rerr == nil {
			navLabels = append(navLabels, text)
		}
	}

	var res UITitleResult
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".tsx" {
			continue
		}
		comp := strings.TrimSuffix(e.Name(), ".tsx")
		route, mounted := routes[comp]
		if !mounted {
			continue
		}
		src, rerr := os.ReadFile(filepath.Join(pagesDir, e.Name()))
		if rerr != nil {
			return UITitleResult{}, fmt.Errorf("ui-vocab: read page %s: %w", e.Name(), rerr)
		}
		for _, key := range ExtractPageHeaderTitles(string(src)) {
			text, rerr := ResolveLocale(locale, key)
			if rerr != nil {
				continue // unresolvable key: skip rather than propose a broken assertion
			}
			c := UITitleCandidate{Route: route, Component: comp, I18nKey: key, Text: text, SourceFile: e.Name()}
			if CollidesWithNav(text, navLabels) {
				res.Flagged = append(res.Flagged, c)
			} else {
				res.Safe = append(res.Safe, c)
			}
		}
	}
	return res, nil
}

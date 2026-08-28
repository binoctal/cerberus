package vocabextract

import (
	"os"
	"path/filepath"
	"testing"
)

const routerFixture = `
export function App() {
  return (
    <Routes>
      <Route path="/dashboard" element={
        <ProtectedRoute>
          <DashboardRoutes />
        </ProtectedRoute>
      }>
        <Route index element={<DashboardPage />} />
        <Route path="agents" element={<AgentsPage />} />
        <Route path="logs" element={<LogsPage />} />
        <Route path="devices/:id" element={<DeviceDetailPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}
`

func TestExtractDashboardRoutes(t *testing.T) {
	got := ExtractDashboardRoutes(routerFixture)
	want := map[string]string{
		"DashboardPage":    "/dashboard",
		"AgentsPage":       "/dashboard/agents",
		"LogsPage":         "/dashboard/logs",
		"DeviceDetailPage": "/dashboard/devices/:id",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d routes, want %d: %+v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}
	// The unrelated top-level "*" route (outside the /dashboard block) must
	// not leak in.
	if _, ok := got["Navigate"]; ok {
		t.Error("route outside the /dashboard block leaked into the map")
	}
}

func TestExtractDashboardRoutesNoMatch(t *testing.T) {
	if got := ExtractDashboardRoutes("no routes here"); len(got) != 0 {
		t.Errorf("want empty map, got %+v", got)
	}
}

const agentsPageFixture = `
export function AgentsPage() {
  return (
    <div>
      <PageHeader
        title={t('agent.title')}
        description={t('agent.subtitle')}
        actions={
          <div className="flex gap-2">
            <Button>{t('agent.tagManager')}</Button>
          </div>
        }
      />
    </div>
  )
}
`

const noHeaderFixture = `
export function DevicesPage() {
  return <div className="grid">{devices.map(d => <Card key={d.id} />)}</div>
}
`

func TestExtractPageHeaderTitles(t *testing.T) {
	got := ExtractPageHeaderTitles(agentsPageFixture)
	if len(got) != 1 || got[0] != "agent.title" {
		t.Fatalf("got %v, want [agent.title]", got)
	}
	if got := ExtractPageHeaderTitles(noHeaderFixture); len(got) != 0 {
		t.Errorf("page without PageHeader must yield nothing, got %v", got)
	}
}

func TestExtractPageHeaderTitlesSingleLine(t *testing.T) {
	src := `<PageHeader title={t('nav.search')} subtitle={t('search.subtitle')} />`
	got := ExtractPageHeaderTitles(src)
	if len(got) != 1 || got[0] != "nav.search" {
		t.Fatalf("got %v, want [nav.search]", got)
	}
}

const localeFixture = `{
  "agent": {"title": "Custom Agents", "subtitle": "Create personalized AI assistants"},
  "nav": {"agents": "Agents", "logs": "Logs"},
  "logs": {"center": "Logs Center"}
}`

func TestResolveLocale(t *testing.T) {
	cases := []struct {
		key  string
		want string
	}{
		{"agent.title", "Custom Agents"},
		{"nav.agents", "Agents"},
		{"logs.center", "Logs Center"},
	}
	for _, c := range cases {
		got, err := ResolveLocale([]byte(localeFixture), c.key)
		if err != nil {
			t.Fatalf("%s: %v", c.key, err)
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.key, got, c.want)
		}
	}
}

func TestResolveLocaleErrors(t *testing.T) {
	if _, err := ResolveLocale([]byte(localeFixture), "agent.missing"); err == nil {
		t.Error("missing key must error, not silently return empty")
	}
	if _, err := ResolveLocale([]byte(localeFixture), "agent"); err == nil {
		t.Error("non-string leaf (an object) must error")
	}
	if _, err := ResolveLocale([]byte("not json"), "x"); err == nil {
		t.Error("malformed JSON must error")
	}
}

const navFixture = `
const NAV_ITEMS = [
  { path: "/dashboard", icon: "dashboard", labelKey: "nav.dashboard", exact: true },
  { path: "/dashboard/agents", icon: "smart_toy", labelKey: "nav.agents" },
  { path: "/dashboard/logs", icon: "history", labelKey: "nav.logs" },
]

function TopBar() {
  return <Link to="/dashboard/search">{t('layout.search')}</Link>
}
`

func TestExtractNavLabelKeys(t *testing.T) {
	got := ExtractNavLabelKeys(navFixture)
	want := []string{"nav.dashboard", "nav.agents", "nav.logs", "layout.search"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExtractNavLabelKeysDedup(t *testing.T) {
	src := `labelKey: "nav.x"` + "\n" + `t('nav.x')`
	if got := ExtractNavLabelKeys(src); len(got) != 1 {
		t.Errorf("same key from both patterns must dedup, got %v", got)
	}
}

func TestCollidesWithNav(t *testing.T) {
	nav := []string{"Agents", "Logs", "Settings"}
	if !CollidesWithNav("Agents", nav) {
		t.Error("exact nav match must collide")
	}
	if CollidesWithNav("Custom Agents", nav) {
		t.Error("distinct page-title string must not collide with a nav label")
	}
}

// TestExtractUITitleCandidatesFromDisk exercises the full orchestrator
// against a minimal on-disk fixture: one safe page, one colliding page, one
// page not mounted under /dashboard (must be silently skipped).
func TestExtractUITitleCandidatesFromDisk(t *testing.T) {
	dir := t.TempDir()
	pagesDir := filepath.Join(dir, "pages")
	if err := os.Mkdir(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writePage := func(name, content string) {
		if err := os.WriteFile(filepath.Join(pagesDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	write("App.tsx", routerFixture)
	write("Layout.tsx", navFixture)
	write("en.json", `{"agent":{"title":"Custom Agents"},"nav":{"dashboard":"Dashboard","agents":"Agents","logs":"Logs"},"logs":{"colliding":"Agents"}}`)
	writePage("AgentsPage.tsx", agentsPageFixture)
	// LogsPage's title resolves to the SAME text as the nav's "Agents"
	// label — an artificial collision to prove the flagged path.
	writePage("LogsPage.tsx", `<PageHeader title={t('logs.colliding')} />`)
	// Not mounted under /dashboard in routerFixture — must be skipped, not
	// guessed at.
	writePage("UnmountedPage.tsx", `<PageHeader title={t('agent.title')} />`)

	res, err := ExtractUITitleCandidatesFromDisk(
		filepath.Join(dir, "App.tsx"), pagesDir, filepath.Join(dir, "Layout.tsx"), filepath.Join(dir, "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Safe) != 1 || res.Safe[0].Component != "AgentsPage" || res.Safe[0].Text != "Custom Agents" {
		t.Fatalf("safe: %+v", res.Safe)
	}
	if len(res.Flagged) != 1 || res.Flagged[0].Component != "LogsPage" {
		t.Fatalf("flagged: %+v", res.Flagged)
	}
}

func TestExtractPageHeaderTitlesDeduplicatesRepeatedKey(t *testing.T) {
	// Real shape (PromptLabPage.tsx, found 2026-08-28): one page renders the
	// SAME PageHeader title key in two layout branches. One key = one
	// assertion — a repeat would emit a duplicate assertion id.
	src := `
<div branch="a">
  <PageHeader
    title={t('promptLab.title')}
    subtitle={t('promptLab.subtitle')}
  />
</div>
<div branch="b">
  <PageHeader
    title={t('promptLab.title')}
    subtitle={t('promptLab.subtitle')}
  />
</div>`
	got := ExtractPageHeaderTitles(src)
	if len(got) != 1 || got[0] != "promptLab.title" {
		t.Fatalf("got %v, want exactly [promptLab.title]", got)
	}
}

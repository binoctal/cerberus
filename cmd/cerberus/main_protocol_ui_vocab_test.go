package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunProtocolUIVocab(t *testing.T) {
	dir := t.TempDir()
	pagesDir := filepath.Join(dir, "pages")
	if err := os.Mkdir(pagesDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(rel, content string) {
		if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write("App.tsx", `
<Route path="/dashboard" element={<X/>}>
  <Route path="agents" element={<AgentsPage />} />
</Route>
`)
	write("Layout.tsx", `const items = [{ path: "/dashboard/agents", labelKey: "nav.agents" }]`)
	write("en.json", `{"agent":{"title":"Custom Agents"},"nav":{"agents":"Agents"}}`)
	write("pages/AgentsPage.tsx", `<PageHeader title={t('agent.title')} />`)

	var buf bytes.Buffer
	err := runProtocolUIVocab(
		filepath.Join(dir, "App.tsx"), pagesDir, filepath.Join(dir, "Layout.tsx"), filepath.Join(dir, "en.json"), &buf)
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "id: agents-title") {
		t.Errorf("missing safe candidate in output:\n%s", out)
	}
	if !strings.Contains(out, `target: "text=Custom Agents"`) {
		t.Errorf("missing resolved text in output:\n%s", out)
	}
	if strings.Contains(out, "FLAGGED") {
		t.Errorf("no collisions expected, got a FLAGGED section:\n%s", out)
	}
}

func TestRunProtocolUIVocab_MissingFile(t *testing.T) {
	var buf bytes.Buffer
	if err := runProtocolUIVocab("/nope/App.tsx", "/nope/pages", "/nope/Layout.tsx", "/nope/en.json", &buf); err == nil {
		t.Fatal("missing router file must error, not silently produce empty output")
	}
}

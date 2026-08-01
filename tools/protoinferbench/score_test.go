package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/binoctal/cerberus/internal/project"
)

func mustLoad(t *testing.T, name string) *project.Protocol {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var p project.Protocol
	if err := yaml.Unmarshal(b, &p); err != nil {
		t.Fatalf("unmarshal %s: %v", name, err)
	}
	return &p
}

func TestScore(t *testing.T) {
	run2 := mustLoad(t, "run2-draft.yaml")
	run22 := mustLoad(t, "run22-like-draft.yaml")
	// run2: envelope + roles + auth only.
	wantRun2 := [numStructures]bool{true, true, true, true, false, false, false}
	// run22: all but batch_items_path (batch.lines != payload.lines).
	wantRun22 := [numStructures]bool{true, true, true, true, true, true, false}

	for _, tc := range []struct {
		name string
		p    *project.Protocol
		want [numStructures]bool
	}{
		{"run2", run2, wantRun2},
		{"run22", run22, wantRun22},
		{"nil", nil, [numStructures]bool{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Score(tc.p)
			if got != tc.want {
				t.Fatalf("Score(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestParseDraft(t *testing.T) {
	t.Run("valid prefixed", func(t *testing.T) {
		raw := "Draft protocol \"open-agents\":\nframing: json\ntype_path: type\n"
		p, err := ParseDraft(raw)
		if err != nil || p == nil {
			t.Fatalf("got (%p, %v), want parsed proto", p, err)
		}
		if p.Framing != "json" || p.TypePath != "type" {
			t.Fatalf("parsed fields wrong: %+v", p)
		}
	})
	t.Run("no draft line", func(t *testing.T) {
		if _, err := ParseDraft("no WebSocket protocol found in the provided inputs\n"); err != errNoDraft {
			t.Fatalf("got %v, want errNoDraft", err)
		}
	})
	t.Run("corrupt yaml", func(t *testing.T) {
		raw := "Draft protocol \"x\":\nframing: [unclosed\n"
		if _, err := ParseDraft(raw); err == nil {
			t.Fatalf("want parse error, got nil")
		}
	})
}

func TestClassifyRun(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stdout   string
		exitCode int
		outcome  runOutcome
		hasProto bool
	}{
		{"draft", "Draft protocol \"x\":\nframing: json\n", 0, outcomeDraft, true},
		{"no_protocol", "no WebSocket protocol found in the provided inputs\n", 0, outcomeNoProtocol, false},
		{"parse_fail", "Draft protocol \"x\":\nframing: [bad\n", 0, outcomeParseFail, false},
		{"hard_error", "Error: model produced an invalid protocol\n", 1, outcomeHardError, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := classifyRun(tc.stdout, "stderr", tc.exitCode)
			if r.outcome != tc.outcome {
				t.Fatalf("outcome = %s, want %s", r.outcome, tc.outcome)
			}
			if r.hasProto() != tc.hasProto {
				t.Fatalf("hasProto = %v, want %v", r.hasProto(), tc.hasProto)
			}
		})
	}
}

func TestAggregate(t *testing.T) {
	good := mustLoad(t, "run22-like-draft.yaml")
	// 4 runs: 2 good drafts, 1 no_protocol, 1 hard_error -> N=4.
	results := []runResult{
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeNoProtocol},
		{outcome: outcomeHardError},
	}
	rep := Aggregate(results, "3")
	if rep.n != 4 {
		t.Fatalf("n = %d, want 4", rep.n)
	}
	// run22 hits 6/7 structures; over 2 good drafts -> 2 hits each, denom 4.
	wantHits := map[string]int{
		"framing": 2, "type_path": 2, "auth": 2, "roles": 2,
		"handshake": 2, "batch_keys": 2, "batch_items_path": 0,
	}
	got := map[string]int{}
	for _, s := range rep.structures {
		got[s.name] = s.hits
	}
	for name, want := range wantHits {
		if got[name] != want {
			t.Fatalf("hits[%s] = %d, want %d", name, got[name], want)
		}
	}
	// batch_items_path: 0/4 = 0% < 40% threshold -> overall FAIL.
	if rep.overall {
		t.Fatalf("overall = PASS, want FAIL (batch_items_path below threshold)")
	}
}

func TestFormatReport(t *testing.T) {
	good := mustLoad(t, "run22-like-draft.yaml")
	results := []runResult{
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeDraft, proto: good},
		{outcome: outcomeNoProtocol},
		{outcome: outcomeHardError},
	}
	rep := Aggregate(results, "3")
	out := formatReport(rep)
	for _, want := range []string{
		"N=4", "samples=3 per run", "| Structure",
		"framing", "batch_items_path", "Overall:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q; got:\n%s", want, out)
		}
	}
	// Overall verdict line: 6/7 structures pass (batch_items_path fails), so
	// the overall verdict is FAIL and the count reflects numStructures.
	for _, want := range []string{"Overall: FAIL (", "/7 structures)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q; got:\n%s", want, out)
		}
	}
}

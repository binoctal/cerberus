package scout

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/project"
)

func TestVocabTypeSet(t *testing.T) {
	v := &project.Vocabulary{Edges: []project.VocabEdge{
		{Type: "session:start"},
		{Type: "device:online"},
		{Type: "session:start"}, // duplicate must be collapsed
	}}
	set := vocabTypeSet(v)
	if len(set) != 2 || !set["session:start"] || !set["device:online"] {
		t.Fatalf("vocabTypeSet = %v, want {session:start, device:online}", set)
	}
}

func TestVocabTypeSetNilSafe(t *testing.T) {
	if got := vocabTypeSet(nil); len(got) != 0 {
		t.Fatalf("vocabTypeSet(nil) = %v, want empty", got)
	}
}

func TestDumpPlanContainsReceiveType(t *testing.T) {
	plan := &agent.TestPlan{Goal: "relay", Cases: []agent.TestCase{
		{ID: "tc-1", Steps: []agent.TestStep{
			{Action: "ws_receive", Type: "session:created"},
		}},
	}}
	out := dumpPlan(plan)
	if !strings.Contains(out, "session:created") {
		t.Fatalf("dump missing ws_receive type, got:\n%s", out)
	}
	// Must be valid JSON so downstream extraction is stable.
	var check agent.TestPlan
	if err := json.Unmarshal([]byte(out), &check); err != nil {
		t.Fatalf("dump is not valid TestPlan JSON: %v", err)
	}
}

func TestExtractTypeTokens(t *testing.T) {
	in := `send {"type":"session:start"} then expect device:online and session:output-batch; session:start again`
	tokens := extractTypeTokens(in)
	got := map[string]bool{}
	for _, tk := range tokens {
		got[tk] = true
	}
	want := map[string]bool{"session:start": true, "device:online": true, "session:output-batch": true}
	for k := range want {
		if !got[k] {
			t.Fatalf("extractTypeTokens missing %q, got %v", k, tokens)
		}
	}
	// Dedup: "session:start" appears twice in the input but once in the slice.
	seen := map[string]int{}
	for _, tk := range tokens {
		seen[tk]++
	}
	for tk, n := range seen {
		if n > 1 {
			t.Fatalf("extractTypeTokens has duplicate %q (%d)", tk, n)
		}
	}
}

func TestClassifyTypes(t *testing.T) {
	set := map[string]bool{"session:start": true, "device:online": true}
	hits, invented := classifyTypes([]string{"session:start", "message:received", "device:online"}, set)
	if len(hits) != 2 {
		t.Fatalf("hits = %v, want 2", hits)
	}
	if len(invented) != 1 || invented[0] != "message:received" {
		t.Fatalf("invented = %v, want [message:received]", invented)
	}
}

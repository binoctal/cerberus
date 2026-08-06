//go:build manual

package examiner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/agent"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

// TestExaminerVocabValidation measures judge drift with vs without the WS
// routing vocabulary, on a fixed set of WS relay cases whose ground truth is
// "pass". The case set spans expectations from precise (naming the type) to
// vague ("web should get the update"); the vocabulary should anchor the judge
// to concrete legal types and reduce drift (non-pass or low-confidence
// verdicts) especially on the vague cases.
//
// Run manually:
//
//	go test -tags=manual ./internal/head/examiner/ -run TestExaminerVocabValidation -v
//
// Credentials resolve exactly like the Scout manual validation
// (internal/head/scout/vocab_validation_manual_test.go): env only, populated by
// .claude/settings.json. The //go:build manual line keeps this out of the
// default build and CI; it skips (does not fail) when no credential resolves.
func TestExaminerVocabValidation(t *testing.T) {
	model := firstNonEmpty(os.Getenv("CERBERUS_LLM_MODEL"), os.Getenv("ANTHROPIC_DEFAULT_SONNET_MODEL"))
	apiKey, scheme := resolveLLMCred()
	if model == "" || apiKey == "" {
		t.Skip("no LLM credential resolved (set .claude/settings.json or CERBERUS_LLM_* / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_API_KEY)")
	}
	baseURL := firstNonEmpty(os.Getenv("CERBERUS_LLM_BASE_URL"), os.Getenv("ANTHROPIC_BASE_URL"))
	t.Logf("model=%s baseURL=%q authScheme=%s", model, baseURL, scheme)

	// Load the real ws-realtime vocabulary to (a) render the vocab summary for
	// the with-vocab condition and (b) ground the synthetic evidence in real
	// protocol types.
	cfgPath := filepath.Join("..", "..", "..", "dogfood", "ws-realtime", ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Services, "dogfood config must declare a service")
	require.NotNil(t, cfg.Services[0].Vocabulary, "dogfood config must auto-load a vocabulary")
	vocabSummary := project.RenderVocabSummary(cfg.Services)
	require.NotEmpty(t, vocabSummary, "vocab summary must render for ws-realtime")
	t.Logf("vocab summary: %d chars", len(vocabSummary))

	cases := buildValidationCases()
	outDir := filepath.Join("..", "..", "..", "runtime", "examiner-vocab-validation")
	require.NoError(t, os.MkdirAll(outDir, 0o0755))

	const runsPerCondition = 3
	// The condition matrix crosses the vocabulary intervention (with/without)
	// with the dimension intervention (derived vs stripped). The dimension's
	// independent effect is the +dim vs +strip contrast within one vocab row;
	// the vocab effect is the column contrast. N=3 each.
	conditions := []struct {
		name    string
		summary string
		derive  bool
	}{
		{"vocab-dim", vocabSummary, true},
		{"vocab-strip", vocabSummary, false},
		{"novocab-dim", "", true},
		{"novocab-strip", "", false},
	}

	var report string
	const driftThreshold = 0.9 // drift = Status != pass OR CorrectnessConfidence < threshold
	for _, cond := range conditions {
		for run := 1; run <= runsPerCondition; run++ {
			label := fmt.Sprintf("%s-run%d", cond.name, run)
			t.Run(label, func(t *testing.T) {
				client, err := llm.NewClientWithConfig(llm.ClientConfig{
					Model:      model,
					APIKey:     apiKey,
					BaseURL:    baseURL,
					Provider:   os.Getenv("CERBERUS_LLM_PROVIDER"),
					AuthScheme: scheme,
				})
				require.NoError(t, err)
				driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))
				judge := NewJudge(driver, nil, ExaminerConfig{
					ConfThreshold: driftThreshold,
					VocabSummary:  cond.summary,
				})
				judge.deriveEnabled = cond.derive

				var incorrect, honest, underconf int
				var lines []string
				for _, c := range cases {
					vr, err := judge.Judge(context.Background(), c.result)
					require.NoError(t, err, "judge failed for case %q", c.name)
					cat := classifyDrift(vr.Status, vr.CorrectnessConfidence, driftThreshold)
					switch cat {
					case "incorrect":
						incorrect++
					case "honest-uncertain":
						honest++
					case "under-confident":
						underconf++
					}
					lines = append(lines, fmt.Sprintf("  %-14s status=%-9s conf=%.2f cat=%s", c.name, vr.Status, vr.CorrectnessConfidence, cat))
				}
				oldDrift := incorrect + honest + underconf
				newDrift := incorrect + underconf
				summary := fmt.Sprintf("[%s] cases=%d incorrect=%d honest=%d underconf=%d new_drift=%d old_drift=%d",
					label, len(cases), incorrect, honest, underconf, newDrift, oldDrift)
				t.Log(summary)
				for _, l := range lines {
					t.Log(l)
				}
				report += summary + "\n"
				require.NoError(t, os.WriteFile(
					filepath.Join(outDir, label+".txt"), []byte(summary+"\n"+joinLines(lines)), 0o644))
			})
		}
	}
	// Overall drift tally per condition.
	require.NoError(t, os.WriteFile(filepath.Join(outDir, "summary.txt"), []byte(report), 0o644))
	t.Logf("wrote summary to %s", filepath.Join(outDir, "summary.txt"))
}

type validationCase struct {
	name   string
	result agent.StepResult
}

// buildValidationCases returns a fixed set of WS relay StepResults whose ground
// truth is pass. Evidence is synthetic but uses real protocol types and the
// exact frame format buildEvidenceContext surfaces to the judge. Expectations
// range from precise (naming the type) to deliberately vague — the vague ones
// are where the vocabulary summary should most reduce judge drift.
func buildValidationCases() []validationCase {
	mk := func(id, expectation, matchedType string, payload string) validationCase {
		msg := fmt.Sprintf(`{"type":%q,"payload":%s}`, matchedType, payload)
		return validationCase{name: id, result: agent.StepResult{
			TestCase: &agent.TestCase{
				ID: "vc-" + id, Name: id, Target: "ws://localhost:8989/ws",
				Expectation: expectation,
			},
			Status:   agent.StepPassed,
			Attempts: 1,
			Result: types.WSResult{
				OK:             true,
				MatchedMessage: msg,
				MatchedCount:   1,
			},
		}}
	}
	return []validationCase{
		mk("precise", "web receives a workflow:task_progress broadcast from bridge",
			"workflow:task_progress", `{"taskId":"t1","pct":42}`),
		mk("vague", "web should get the running task update pushed to it",
			"workflow:task_progress", `{"taskId":"t1","pct":42}`),
		mk("routing", "every connected web peer except the sender receives the broadcast",
			"workflow:task_progress", `{"taskId":"t1","pct":50}`),
		mk("lifecycle", "web is told a session was established",
			"session:created", `{"sessionId":"s1"}`),
		// fanout carries a real per-step trace so deriveDimensions yields a
		// membership dimension (sender + 2 recipients). The with-dimensions
		// condition renders that dimension; the strip condition drops it. This
		// case is the direct measurement of the dimension's effect on drift.
		{
			name: "fanout",
			result: agent.StepResult{
				TestCase: &agent.TestCase{
					ID: "vc-fanout", Name: "fanout", Target: "ws://localhost:8989/ws",
					Expectation: "the broadcast reaches both other web peers",
				},
				Status:   agent.StepPassed,
				Attempts: 1,
				Result: types.WSResult{
					OK:             true,
					MatchedMessage: `{"type":"workflow:task_progress","payload":{"pct":50}}`,
					MatchedCount:   1,
				},
				Evidence: []agent.Evidence{
					{Action: "ws_send", ConnectionID: "c-web", MatchedType: "workflow:task_progress", Content: "ws_send: ok"},
					{Action: "ws_receive", ConnectionID: "c-bridge", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
					{Action: "ws_receive", ConnectionID: "c-web-2", MatchedType: "workflow:task_progress", Matched: true, Content: "ws_receive: matched"},
				},
			},
		},
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// firstNonEmpty returns its first non-empty argument, or "".
func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveLLMCred mirrors internal/config.resolveAPIKeyWithScheme's env path:
// CERBERUS_LLM_API_KEY (x-api-key) > ANTHROPIC_AUTH_TOKEN (bearer) >
// ANTHROPIC_API_KEY (x-api-key). The auth scheme tracks the source so the GLM
// relay's auth token is sent as Authorization: Bearer.
func resolveLLMCred() (string, llm.AuthScheme) {
	if k := os.Getenv("CERBERUS_LLM_API_KEY"); k != "" {
		return k, llm.AuthSchemeAPIKey
	}
	if k := os.Getenv("ANTHROPIC_AUTH_TOKEN"); k != "" {
		return k, llm.AuthSchemeBearer
	}
	return os.Getenv("ANTHROPIC_API_KEY"), llm.AuthSchemeAPIKey
}

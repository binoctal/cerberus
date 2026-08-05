//go:build manual

package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// TestVocabValidation_ToT dumps Scout (ToT) planning output for the dogfood
// ws-realtime config, with and without the routing vocabulary, N=3 runs each.
// It classifies every namespace:action token in each dump as hit (in the real
// vocab) or invented, and writes the dumps under runtime/vocab-validation/.
//
// Run manually:
//
//	go test -tags=manual ./internal/head/scout/ -run TestVocabValidation_ToT -v
//
// Credentials mirror the production resolver (internal/config) but read env
// only, since Claude Code injects .claude/settings.json into the process env:
// model from CERBERUS_LLM_MODEL or ANTHROPIC_DEFAULT_SONNET_MODEL; key from
// CERBERUS_LLM_API_KEY / ANTHROPIC_AUTH_TOKEN (bearer) / ANTHROPIC_API_KEY;
// base URL from CERBERUS_LLM_BASE_URL or ANTHROPIC_BASE_URL. This reaches the
// same GLM relay or direct Anthropic endpoint the binary uses. The //go:build
// manual line keeps this file out of the default build and CI; under
// -tags=manual it still skips (does not fail) when no credential resolves.
func TestVocabValidation_ToT(t *testing.T) {
	model := firstNonEmpty(os.Getenv("CERBERUS_LLM_MODEL"), os.Getenv("ANTHROPIC_DEFAULT_SONNET_MODEL"))
	apiKey, scheme := resolveLLMCred()
	if model == "" || apiKey == "" {
		t.Skip("no LLM credential resolved (set .claude/settings.json or CERBERUS_LLM_* / ANTHROPIC_AUTH_TOKEN / ANTHROPIC_API_KEY)")
	}
	baseURL := firstNonEmpty(os.Getenv("CERBERUS_LLM_BASE_URL"), os.Getenv("ANTHROPIC_BASE_URL"))
	t.Logf("model=%s baseURL=%q authScheme=%s", model, baseURL, scheme)

	cfgPath := filepath.Join("..", "..", "..", "dogfood", "ws-realtime", ".cerberus", "project.yaml")
	cfg, err := project.LoadFromFile(cfgPath)
	require.NoError(t, err)
	require.NotEmpty(t, cfg.Services, "dogfood config must declare a service")
	require.NotNil(t, cfg.Services[0].Vocabulary, "dogfood config must auto-load a vocabulary")

	realVocab := cfg.Services[0].Vocabulary
	typeSet := vocabTypeSet(realVocab)
	t.Logf("real vocabulary: %d distinct types", len(typeSet))

	const goal = "Cover the realtime WebSocket service's message relay between web and bridge actors: session lifecycle, bridge join/leave signaling, and workflow task progress broadcast. Author WS choreography that drives messages from each role and asserts what each peer receives."

	outDir := filepath.Join("..", "..", "..", "runtime", "vocab-validation")
	require.NoError(t, os.MkdirAll(outDir, 0o755))

	const runsPerCondition = 3
	conditions := []struct {
		name  string
		vocab *project.Vocabulary
	}{
		{"with-vocab", realVocab},
		{"without-vocab", nil},
	}

	for _, cond := range conditions {
		for run := 1; run <= runsPerCondition; run++ {
			label := fmt.Sprintf("%s-run%d", cond.name, run)
			t.Run(label, func(t *testing.T) {
				runCfg := cloneConfigWithVocab(cfg, cond.vocab)
				store := setupTestStore(t)

				client, err := llm.NewClientWithConfig(llm.ClientConfig{
					Model:      model,
					APIKey:     apiKey,
					BaseURL:    baseURL,
					Provider:   os.Getenv("CERBERUS_LLM_PROVIDER"),
					AuthScheme: scheme,
				})
				require.NoError(t, err)
				driver := ai.NewDriver(client, ai.NewTokenBudget(200000, 10000))

				sct := NewScout(driver, store, runCfg, zap.NewNop())
				sct.SetDeepPlan(DefaultToTConfig(), driver, driver)

				pm := sct.buildModelFromConfig()
				plan, err := sct.Plan(context.Background(), goal, pm)
				require.NoError(t, err)

				dump := dumpPlan(plan)
				require.NoError(t, os.WriteFile(
					filepath.Join(outDir, label+".md"), []byte(dump), 0o644))

				tokens := extractTypeTokens(scanFields(dump))
				hits, invented := classifyTypes(tokens, typeSet)
				t.Logf("[%s] cases=%d tokens=%d hits=%d invented=%d invented-list=%v",
					label, len(plan.Cases), len(tokens), len(hits), len(invented), invented)
			})
		}
	}
}

// cloneConfigWithVocab returns a shallow copy of cfg whose services carry the
// given vocabulary (pass nil to simulate a missing vocab file). Used here to
// produce byte-equivalent with/without conditions from one loaded config.
func cloneConfigWithVocab(cfg *project.Config, vocab *project.Vocabulary) *project.Config {
	c := *cfg
	svcs := make([]project.Service, len(cfg.Services))
	for i, s := range cfg.Services {
		s.Vocabulary = vocab
		svcs[i] = s
	}
	c.Services = svcs
	return &c
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

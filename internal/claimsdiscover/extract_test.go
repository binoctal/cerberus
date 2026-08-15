package claimsdiscover

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// mockExtractDriver wires a canned claims_extract tool-call payload into the
// ai.Driver so Extract runs without any network (mirrors mockProtocolDriver).
func mockExtractDriver(claims []any) *ai.Driver {
	mock := llm.NewMockClient(nil)
	mock.SetToolResponse("default", []llm.ToolCall{
		{Name: "claims_extract", Input: map[string]any{"claims": claims}},
	})
	return ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000))
}

func cannedClaim(id, text string) map[string]any {
	return map[string]any{"id": id, "text": text}
}

func TestExtract_ParsesCannedPayload(t *testing.T) {
	claims := []any{
		map[string]any{"id": "multi-bridge", "text": "Multiple bridge devices pair via a dev backdoor", "implies_cardinality": 2},
		cannedClaim("session-output", "Session output flushes per line"),
	}
	got, err := Extract(context.Background(), mockExtractDriver(claims),
		map[string]string{"README.md": "# readme"}, 15)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "multi-bridge", got[0].ID)
	assert.Equal(t, 2, got[0].ImpliesCardinality)
	assert.Equal(t, "session-output", got[1].ID)
}

func TestExtract_EnforcesMax(t *testing.T) {
	var claims []any
	for i := 0; i < 20; i++ {
		claims = append(claims, cannedClaim(fmt.Sprintf("claim-%d", i), fmt.Sprintf("claim %d text", i)))
	}
	// Explicit cap truncates.
	got, err := Extract(context.Background(), mockExtractDriver(claims), nil, 3)
	require.NoError(t, err)
	assert.Len(t, got, 3)
	// max<=0 falls back to DefaultMaxClaims.
	got, err = Extract(context.Background(), mockExtractDriver(claims), nil, 0)
	require.NoError(t, err)
	assert.Len(t, got, DefaultMaxClaims)
}

func TestExtract_SkipsInvalidEntries(t *testing.T) {
	claims := []any{
		cannedClaim("ok-claim", "A real capability claim"),
		cannedClaim("Bad_ID", "invalid id form"), // not kebab-case
		cannedClaim("empty-text", ""),            // no text
		map[string]any{"text": "no id at all"},   // no id
		cannedClaim("dup-claim", "first"),        // duplicate id — second dropped
		cannedClaim("dup-claim", "second"),
	}
	got, err := Extract(context.Background(), mockExtractDriver(claims), nil, 15)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "ok-claim", got[0].ID)
	assert.Equal(t, "dup-claim", got[1].ID)
	assert.Equal(t, "first", got[1].Text)
}

func TestExtract_NoToolCallIsNoClaims(t *testing.T) {
	mock := llm.NewMockClient(nil)
	_, err := Extract(context.Background(), ai.NewDriver(mock, ai.NewTokenBudget(200000, 10000)), nil, 15)
	assert.ErrorIs(t, err, ErrNoClaims)
}

// triageFixture is a dogfood-shaped config exercising every surface token
// family: service URL host, protocol role/message types, actor name, process
// command.
func triageFixture() *project.Config {
	return &project.Config{
		Services: []project.Service{{
			Name: "realtime",
			URL:  "http://localhost:8989/ws/{userId}",
			Protocol: &project.Protocol{
				Roles: map[string]*project.ProtocolRole{
					"web": {},
				},
				Batches: map[string]*project.ProtocolBatch{
					"session:output-batch": {ItemType: "session:output"},
				},
			},
		}},
		Actors: []project.Actor{{
			Name:     "bridge-pty-1",
			Fidelity: project.FidelityRealProcess,
			Process: &project.ProcessSpec{
				Workdir: "../../bridge",
				Start:   []string{"./build/open-agents-bridge", "start"},
			},
		}},
	}
}

func TestSurfaceTriage(t *testing.T) {
	draft := []project.Claim{
		{ID: "bridge-pairing", Text: "bridge-pty-1 devices pair via the dev backdoor"},  // actor name
		{ID: "session-output", Text: "Session output flushes as session:output frames"}, // message type
		{ID: "web-role", Text: "A web connection receives device sync events"},          // protocol role
		{ID: "marketing", Text: "The product feels delightful"},                         // no surface
	}
	got := SurfaceTriage(draft, triageFixture())
	require.Len(t, got, 4)
	for i, want := range []struct {
		critical   bool
		annotation string
	}{
		{true, ""},
		{true, ""},
		{true, ""},
		{false, AnnotationNoSurface},
	} {
		assert.Equal(t, want.critical, got[i].Critical, "claim %s", got[i].ID)
		assert.Equal(t, want.annotation, got[i].StatusAnnotation, "claim %s", got[i].ID)
	}
}

func TestSurfaceTriage_NilCfgAllInformational(t *testing.T) {
	draft := []project.Claim{{ID: "a", Text: "anything at all"}}
	got := SurfaceTriage(draft, nil)
	assert.False(t, got[0].Critical)
	assert.Equal(t, AnnotationNoSurface, got[0].StatusAnnotation)
}

func mergeFixture() *project.ClaimsFile {
	cf := &project.ClaimsFile{}
	cf.Claims = []project.Claim{
		{ID: "keep-me", Text: "kept claim", Critical: true, StatusAnnotation: "wont-test(manual triage)"},
		{ID: "stale-me", Text: "stale claim"},
	}
	return cf
}

func triagedDraft() []project.Claim {
	return []project.Claim{
		// Re-extracted: triage reset Critical/annotation, merge must restore.
		{ID: "keep-me", Text: "kept claim", Critical: false, StatusAnnotation: AnnotationNoSurface},
		{ID: "fresh-me", Text: "brand new claim", Critical: true},
	}
}

func TestMergeClaims_PreservesAndAppends(t *testing.T) {
	merged := MergeClaims(mergeFixture(), triagedDraft(), false)
	require.Len(t, merged.Claims, 3)
	byID := map[string]project.Claim{}
	for _, c := range merged.Claims {
		byID[c.ID] = c
	}
	// Existing id: manual channels survive re-extraction (vocab merge rule).
	assert.True(t, byID["keep-me"].Critical)
	assert.Equal(t, "wont-test(manual triage)", byID["keep-me"].StatusAnnotation)
	// New id appended as extracted.
	assert.True(t, byID["fresh-me"].Critical)
	// Without prune, the stale id is retained (deletion is explicit).
	assert.Equal(t, "stale claim", byID["stale-me"].Text)
}

func TestMergeClaims_PruneDropsStale(t *testing.T) {
	merged := MergeClaims(mergeFixture(), triagedDraft(), true)
	require.Len(t, merged.Claims, 2)
	for _, c := range merged.Claims {
		assert.NotEqual(t, "stale-me", c.ID, "prune must drop ids absent from draft")
	}
}

func TestMergeClaims_NilExisting(t *testing.T) {
	merged := MergeClaims(nil, triagedDraft(), false)
	require.Len(t, merged.Claims, 2)
	assert.Equal(t, "keep-me", merged.Claims[0].ID)
}

func TestMergeClaims_KeepsSourceBlock(t *testing.T) {
	existing := mergeFixture()
	existing.Source.Files = append(existing.Source.Files, struct {
		Path string `yaml:"path"`
		Hash string `yaml:"hash,omitempty"`
	}{Path: "README.md", Hash: "sha256:abc"})
	merged := MergeClaims(existing, triagedDraft(), false)
	require.Len(t, merged.Source.Files, 1)
	assert.Equal(t, "sha256:abc", merged.Source.Files[0].Hash)
}

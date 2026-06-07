package examiner

import (
	"context"
	"testing"

	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMatcherStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../../migrations")
	require.NoError(t, err)
	return s
}

func TestMatchStrategies_Basic(t *testing.T) {
	s := setupMatcherStore(t)
	ctx := context.Background()

	// Store some strategies.
	_, err := s.StoreProceduralWithType(ctx, "auth_failure", "* returned 401",
		"Refresh token", "test", "auth_failure", "failure")
	require.NoError(t, err)
	_, err = s.StoreProceduralWithType(ctx, "success_pattern", "GET /api/v1/users",
		"Always paginate", "test", "general_failure", "success")
	require.NoError(t, err)

	// Match against a 401 scenario.
	strategies, err := MatchStrategies(ctx, s, "POST /api/v1/users returned 401")
	require.NoError(t, err)

	// Should match the failure strategy (contains "401").
	assert.GreaterOrEqual(t, len(strategies), 1)
	found := false
	for _, st := range strategies {
		if st.Condition == "* returned 401" {
			found = true
			assert.Equal(t, "failure", st.Type)
		}
	}
	assert.True(t, found, "should match auth_failure strategy")
}

func TestMatchStrategies_LimitsEnforced(t *testing.T) {
	s := setupMatcherStore(t)
	ctx := context.Background()

	// Store 3 failure strategies + 2 success strategies.
	for i := 0; i < 3; i++ {
		_, err := s.StoreProceduralWithType(ctx, "failure", "* returned 500",
			"Retry with backoff", "test", "server_error", "failure")
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		_, err := s.StoreProceduralWithType(ctx, "success", "GET /api/v1/users",
			"Paginate results", "test", "general_failure", "success")
		require.NoError(t, err)
	}

	strategies, err := MatchStrategies(ctx, s, "GET /api/v1/users returned 500")
	require.NoError(t, err)

	// Should return max 2 failures + max 1 success = 3 max.
	failures, successes := 0, 0
	for _, st := range strategies {
		if st.Type == "success" {
			successes++
		} else {
			failures++
		}
	}
	assert.LessOrEqual(t, failures, maxFailureStrategies)
	assert.LessOrEqual(t, successes, maxSuccessStrategies)
}

func TestMatchStrategies_NoMatch(t *testing.T) {
	s := setupMatcherStore(t)
	ctx := context.Background()

	_, err := s.StoreProceduralWithType(ctx, "auth", "* returned 401",
		"Refresh token", "test", "auth_failure", "failure")
	require.NoError(t, err)

	strategies, err := MatchStrategies(ctx, s, "GET /api/v1/users returned 200")
	require.NoError(t, err)
	assert.Empty(t, strategies) // 401 pattern shouldn't match 200 response
}

func TestMatchesPattern(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		match   bool
	}{
		{"* returned 401", "POST /api/v1/users returned 401", true},
		{"* returned 401", "GET /health returned 200", false},
		{"POST /api/v1/*", "POST /api/v1/users returned 401", true},
		{"GET /api/v1/users", "GET /api/v1/users returned 200", true},
		{"GET /health", "POST /api/users returned 500", false},
		{"* returned 5??", "GET /api returned 503", true},
		{"*", "anything", false}, // Universal pattern rejected
		{"", "anything", false},  // Empty pattern rejected
		{"* timeout", "GET /api/users timeout", true},
		{"GET * 200", "GET /api/users 200", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+" vs "+tt.target, func(t *testing.T) {
			assert.Equal(t, tt.match, matchesPattern(tt.pattern, tt.target))
		})
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern string
		target  string
		match   bool
	}{
		{"* returned 401", "POST /api/v1/users returned 401", true},
		{"* returned 401", "GET /health returned 200", false},
		{"POST /api/v1/*", "POST /api/v1/users returned 401", true},
		{"* returned 5??", "GET /api returned 503", true},
		{"a*b", "axxb", true},
		{"a*b", "abxc", false},
		{"*timeout*", "connection timeout after 30s", true},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.match, globMatch(tt.pattern, tt.target))
		})
	}
}

func TestFormatStrategiesForPrompt(t *testing.T) {
	strategies := []store.ProceduralMemory{
		{Name: "auth", Condition: "* returned 401", Action: "Refresh token", Effectiveness: 0.8, Type: "failure"},
		{Name: "paginate", Condition: "GET /api/v1/users", Action: "Always paginate", Effectiveness: 0.9, Type: "success"},
	}

	output := FormatStrategiesForPrompt(strategies)
	assert.Contains(t, output, "## Learned Strategies")
	assert.Contains(t, output, "[failure] When * returned 401")
	assert.Contains(t, output, "[success] When GET /api/v1/users")
	assert.Contains(t, output, "80%")
	assert.Contains(t, output, "90%")
}

func TestFormatStrategiesForPrompt_Empty(t *testing.T) {
	output := FormatStrategiesForPrompt(nil)
	assert.Empty(t, output)
}

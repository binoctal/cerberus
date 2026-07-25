package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// contractJSON returns the JSON content used by Scout.BuildCoverageContract
// (Decide parses scope/depth/coverage_gate from this). Scout.Plan is fed via
// tool calls from planToolCalls() in lifecycle_test.go, so the legacy "cases"
// field is no longer needed here. The combinedMockClient delivers both at once.
func contractJSON() string {
	response := map[string]any{
		"depth": "smoke",
		"scope": []string{
			"health endpoints",
			"basic API paths",
		},
		"path_types": []string{
			"happy path",
			"basic validation",
		},
		"error_scope": []string{
			"5xx errors",
			"timeout",
		},
		"boundaries": []string{
			"empty input",
			"large payload",
		},
		"invariants": []map[string]any{
			{
				"id":          "inv-001",
				"description": "response time < 200ms",
			},
		},
		"priorities": map[string]any{
			"high":   []string{"health"},
			"medium": []string{"api"},
		},
		"coverage_gate": map[string]any{
			"module":           "api",
			"line_threshold":   80.0,
			"branch_threshold": 70.0,
		},
	}
	b, _ := json.Marshal(response)
	return string(b)
}

func TestRun_WithCoverageContract(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	mockClient := fullRunClientWithContract()
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "verify service health with coverage contract",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".", // Use current directory to allow actual execution
		CoverageFn: stubCoverageFn(),
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err, "Run should complete without error")

	// Verify coverage contract was created and populated
	assert.NotNil(t, sess.Contract, "Contract should be set after Run")
	assert.NotEmpty(t, sess.Contract.Depth, "Contract depth should be populated")
	assert.Equal(t, "smoke", sess.Contract.Depth, "Contract depth should match mock response")
	assert.NotEmpty(t, sess.Contract.Scope, "Contract scope should be populated")
	assert.NotEmpty(t, sess.Contract.CoverageGate.Module, "Coverage gate module should be set")

	// Verify assessment was created (since contract was present)
	assert.NotNil(t, sess.Assessment, "Assessment should be set when Contract exists")

	// Verify summary includes contract and assessment
	dbSess, err := s.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.NotEqual(t, "{}", dbSess.Stats, "Stats JSON should not be empty")

	// Parse stats to verify contract and assessment are serialized
	var stats SessionSummary
	err = json.Unmarshal([]byte(dbSess.Stats), &stats)
	require.NoError(t, err, "Stats JSON should parse into SessionSummary")
	assert.NotNil(t, stats.Contract, "Summary should include contract")
	assert.NotNil(t, stats.Assessment, "Summary should include assessment")

	sess.Close()
}

func TestRun_ContractIntegration_SmokeDepth(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	// Configure smoke coverage depth
	cfg.Settings.Coverage.Depth = "smoke"

	mockClient := fullRunClientWithContract()
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "smoke depth coverage test",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err)

	// Verify contract depth matches config
	assert.NotNil(t, sess.Contract)
	assert.Equal(t, "smoke", sess.Contract.Depth)

	sess.Close()
}

func TestRun_ContractIntegration_NoContractWhenNoCoverage(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	cfg := testConfig()
	// Disable coverage - no contract should be built
	cfg.Settings.Coverage.Depth = "off"

	mockClient := fullRunClient("") // Regular responses
	logger := zap.NewNop()

	sess, err := NewSession(context.Background(), SessionConfig{
		Mode:       ModeRun,
		Goal:       "test without coverage",
		Config:     &cfg,
		Store:      s,
		Client:     mockClient,
		Logger:     logger,
		Gate:       nil,
		ProjectDir: ".",
		CoverageFn: stubCoverageFn(),
	})
	require.NoError(t, err)

	err = sess.Run(context.Background())
	require.NoError(t, err)

	// No contract should be created when coverage is off
	assert.Nil(t, sess.Contract, "Contract should be nil when coverage is off")
	assert.Nil(t, sess.Assessment, "Assessment should be nil when no contract")

	sess.Close()
}

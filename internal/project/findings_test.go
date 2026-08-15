package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindingsRoundtripAndValidate(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cerberus"), 0o755))

	ff := &FindingsFile{Findings: []Finding{{
		ID: "ws-rt-web-csrf-no-origin-a1b2c3d4",
		Summary: "expect_status 403, got 200",
		CaseRef:    "ws-rt-web-csrf-no-origin",
		SessionRef: "sess-1",
		ClaimRefs:  []string{"ws-relay-messaging"},
		Tier:       FindingTierEmulated,
		Status:     FindingOpen,
		FirstSeen:  "2026-08-16T10:00:00Z",
		LastSeen:   "2026-08-16T10:00:00Z",
		Count:      1,
	}}}
	require.NoError(t, SaveFindings(dir, ff))

	loaded, err := LoadFindings(dir)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, ff.Findings[0], loaded.Findings[0])

	t.Run("invalid tier rejected", func(t *testing.T) {
		bad := &FindingsFile{Findings: []Finding{{ID: "x", Summary: "s", Tier: "weird", Status: FindingOpen}}}
		assert.ErrorContains(t, ValidateFindings(bad), "tier")
	})
	t.Run("duplicate ids rejected", func(t *testing.T) {
		bad := &FindingsFile{Findings: []Finding{
			{ID: "x", Summary: "s", Tier: FindingTierReal, Status: FindingOpen},
			{ID: "x", Summary: "s", Tier: FindingTierReal, Status: FindingOpen},
		}}
		assert.ErrorContains(t, ValidateFindings(bad), "duplicate")
	})
	t.Run("missing file is nil-nil", func(t *testing.T) {
		ff, err := LoadFindings(t.TempDir())
		assert.NoError(t, err)
		assert.Nil(t, ff)
	})
}

func TestUpsertFinding(t *testing.T) {
	ff := &FindingsFile{}
	now := "2026-08-16T10:00:00Z"

	// First observation creates the finding.
	changed := UpsertFinding(ff, FindingInput{
		CaseRef: "case-a", ErrorSummary: "close code 1000, want 1009",
		SessionRef: "s1", Tier: FindingTierReal, Now: now,
	})
	assert.True(t, changed)
	require.Len(t, ff.Findings, 1)
	f := ff.Findings[0]
	assert.NotEmpty(t, f.ID)
	assert.Contains(t, f.ID, "case-a")
	assert.Equal(t, 1, f.Count)
	assert.Equal(t, FindingOpen, f.Status)
	assert.Equal(t, FindingTierReal, f.Tier)

	// Same case + same error signature bumps count/last_seen, no new entry.
	later := "2026-08-16T11:00:00Z"
	changed = UpsertFinding(ff, FindingInput{
		CaseRef: "case-a", ErrorSummary: "close code 1000, want 1009",
		SessionRef: "s2", Tier: FindingTierReal, Now: later,
	})
	assert.False(t, changed)
	require.Len(t, ff.Findings, 1)
	assert.Equal(t, 2, ff.Findings[0].Count)
	assert.Equal(t, later, ff.Findings[0].LastSeen)
	assert.Equal(t, "s2", ff.Findings[0].SessionRef, "latest session wins")

	// Same case, different error → distinct finding.
	changed = UpsertFinding(ff, FindingInput{
		CaseRef: "case-a", ErrorSummary: "no close within 10s",
		SessionRef: "s2", Tier: FindingTierReal, Now: later,
	})
	assert.True(t, changed)
	require.Len(t, ff.Findings, 2)
}

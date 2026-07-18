package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRegressionStore(t *testing.T) *RegressionStore {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	require.NoError(t, RunMigrations(context.Background(), s.DB(), "../../migrations"))
	t.Cleanup(func() { _ = s.Close() })
	return NewRegressionStore(s)
}

// Create → Get → List → UpdateResult round-trip, including sql.NullString NULL
// handling. List is filtered to the row we created so it is independent of any
// seed data from later migrations.
func TestRegressionStore_CRUDRoundTrip(t *testing.T) {
	rs := setupRegressionStore(t)
	ctx := context.Background()

	in := &RegressionTest{
		Name:           "auth-flow-happy",
		Category:       "complexity",
		TestType:       "positive",
		ExpectedResult: "200 OK",
		Status:         "pending",
		BugID:          sql.NullString{String: "BUG-1", Valid: true},
	}
	id, err := rs.CreateRegressionTest(ctx, in)
	require.NoError(t, err)
	assert.Greater(t, id, 0)

	got, err := rs.GetRegressionTest(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "auth-flow-happy", got.Name)
	assert.Equal(t, "BUG-1", got.BugID.String)
	assert.True(t, got.BugID.Valid)
	assert.Equal(t, "complexity", got.Category)
	assert.Equal(t, "pending", got.Status)
	assert.False(t, got.Description.Valid, "unset NullString stays NULL")

	// Not-found path.
	_, err = rs.GetRegressionTest(ctx, 999999)
	require.Error(t, err)

	// List filtered by category; locate our row by id (seed-independent).
	listed, err := rs.ListRegressionTests(ctx, "complexity", "")
	require.NoError(t, err)
	var mine *RegressionTest
	for _, lt := range listed {
		if lt.ID == id {
			mine = lt
		}
	}
	require.NotNil(t, mine)
	assert.Equal(t, "auth-flow-happy", mine.Name)

	// Update result; verify status / actual / error / last_run persisted.
	require.NoError(t, rs.UpdateRegressionTestResult(ctx, id, "500 err", "fail", "conn refused"))
	updated, err := rs.GetRegressionTest(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "fail", updated.Status)
	assert.Equal(t, "500 err", updated.ActualResult.String)
	assert.Equal(t, "conn refused", updated.LastError.String)
	assert.True(t, updated.LastRun.Valid)
}

// A row with every optional field unset round-trips with NULLs intact.
func TestRegressionStore_NullFieldsRoundTrip(t *testing.T) {
	rs := setupRegressionStore(t)
	ctx := context.Background()

	in := &RegressionTest{
		Name: "bare", Category: "solid", TestType: "negative",
		ExpectedResult: "error", Status: "pending",
	}
	id, err := rs.CreateRegressionTest(ctx, in)
	require.NoError(t, err)

	got, err := rs.GetRegressionTest(ctx, id)
	require.NoError(t, err)
	assert.False(t, got.BugID.Valid)
	assert.False(t, got.Description.Valid)
	assert.False(t, got.FilePath.Valid)
	assert.False(t, got.Notes.Valid)
}

func TestRegressionStore_CreateBugRecord(t *testing.T) {
	rs := setupRegressionStore(t)
	ctx := context.Background()
	bug := &BugRecord{
		BugID: "BUG-42", Title: "crash", Description: "d", Severity: "high",
		Category: "complexity", AffectedComponent: "auth", Status: "open",
		RootCause: "nil deref", RegressionTestID: 0,
	}
	require.NoError(t, rs.CreateBugRecord(ctx, bug))
}

func TestRegressionStore_AccuracyRoundTrip(t *testing.T) {
	rs := setupRegressionStore(t)
	ctx := context.Background()

	rep := &AccuracyReport{
		RunID: "run-1-unique", Timestamp: time.Now().UTC(), ProjectPath: "/p",
		TotalIssues: 10, TruePositives: 8, FalsePositives: 1, TrueNegatives: 1,
		OverallAccuracy: 0.9,
		ComplexityAcc:   sql.NullFloat64{Float64: 0.88, Valid: true},
	}
	require.NoError(t, rs.TrackAccuracy(ctx, rep))

	hist, err := rs.GetAccuracyHistory(ctx, 50)
	require.NoError(t, err)
	var mine *AccuracyReport
	for _, h := range hist {
		if h.RunID == "run-1-unique" {
			mine = h
		}
	}
	require.NotNil(t, mine)
	assert.Equal(t, 10, mine.TotalIssues)
	assert.Equal(t, 0.9, mine.OverallAccuracy)
	assert.True(t, mine.ComplexityAcc.Valid)
	assert.Equal(t, 0.88, mine.ComplexityAcc.Float64)
	assert.False(t, mine.AbstractionAcc.Valid, "unset NullFloat64 stays NULL")
}

// nullX helpers convert sql.Null* to interface{}: Valid→value, invalid→nil.
func TestNullHelpers(t *testing.T) {
	assert.Nil(t, nullString(sql.NullString{}))
	assert.Equal(t, "x", nullString(sql.NullString{String: "x", Valid: true}))

	ts := time.Now()
	assert.Nil(t, nullTime(sql.NullTime{}))
	assert.Equal(t, ts, nullTime(sql.NullTime{Time: ts, Valid: true}))

	assert.Nil(t, nullFloat64(sql.NullFloat64{}))
	assert.Equal(t, 1.5, nullFloat64(sql.NullFloat64{Float64: 1.5, Valid: true}))
}

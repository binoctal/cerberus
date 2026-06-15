package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunMigrations(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	ctx := context.Background()

	// Verify tables exist (SQLite uses sqlite_master)
	var count int
	err := s.DB().QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 7, "expected at least 7 tables")

	// Idempotent
	err = RunMigrations(ctx, s.DB(), "../../migrations")
	assert.NoError(t, err)
}

func TestMigration_V005_AutoTestReport(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	ctx := context.Background()

	// Verify autotest_report column exists in sessions table
	var columnName string
	err := s.DB().QueryRowContext(ctx,
		`SELECT name FROM pragma_table_info('sessions') WHERE name='autotest_report'`).Scan(&columnName)
	require.NoError(t, err, "autotest_report column should exist after V005 migration")
	assert.Equal(t, "autotest_report", columnName)
}

func TestUpdateSessionAutoTest_RoundTrip(t *testing.T) {
	s := testStoreWithMigrations(t)
	defer func() { _ = s.Close() }()

	ctx := context.Background()

	// Create a test session
	sess, err := s.CreateSession(ctx, "run", "test goal", "test-project")
	require.NoError(t, err)

	// Update with AutoTest report
	testReport := map[string]interface{}{
		"items": []map[string]interface{}{
			{
				"target_file": "foo.go",
				"target_func": "Foo",
				"reason":      "0% covered",
				"test_path":   "foo_test.go",
				"status":      "written",
			},
		},
		"before_coverage_pct": 50.0,
		"after_coverage_pct":  60.0,
	}

	err = s.UpdateSessionAutoTest(ctx, sess.ID, testReport)
	require.NoError(t, err)

	// Read back and verify
	retrieved, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, retrieved.AutoTestReport, "autotest_report should not be empty")

	// Verify JSON can be unmarshaled
	var parsed map[string]interface{}
	err = json.Unmarshal([]byte(retrieved.AutoTestReport), &parsed)
	require.NoError(t, err, "autotest_report should be valid JSON")

	// Verify content
	items, ok := parsed["items"].([]interface{})
	require.True(t, ok, "items should exist")
	assert.Len(t, items, 1, "should have 1 item")
}

// testStoreWithMigrations creates an in-memory SQLite DB and runs all migrations.
func testStoreWithMigrations(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	require.NoError(t, err)
	err = RunMigrations(context.Background(), s.DB(), "../../migrations")
	require.NoError(t, err, "run migrations")
	return s
}

package agent

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/types"

	_ "modernc.org/sqlite"
)

// DatabaseExecutor handles SQL query and assertion actions.
// Supports SQLite (built-in), with optional postgres/mysql drivers.
type DatabaseExecutor struct {
	logger *zap.Logger
}

// NewDatabaseExecutor creates a database executor.
func NewDatabaseExecutor(logger *zap.Logger) *DatabaseExecutor {
	return &DatabaseExecutor{logger: logger}
}

// Execute dispatches database actions.
func (e *DatabaseExecutor) Execute(ctx context.Context, action types.TypedAction) types.ExecutorResult {
	start := time.Now()
	switch a := action.(type) {
	case types.DBQueryAction:
		return e.doQuery(ctx, a, start)
	case types.DBAssertAction:
		return e.doAssert(ctx, a, start)
	default:
		return types.ErrorResult{Err: fmt.Sprintf("db executor: unsupported action %T", action)}
	}
}

func (e *DatabaseExecutor) doQuery(ctx context.Context, a types.DBQueryAction, start time.Time) types.ExecutorResult {
	db, err := sql.Open(a.Driver, a.DSN)
	if err != nil {
		return types.DBResult{OK: false, Driver: a.Driver, Query: a.Query, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, a.Query, a.Args...)
	if err != nil {
		return types.DBResult{OK: false, Driver: a.Driver, Query: a.Query, Err: err.Error(), Latency: time.Since(start)}
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return types.DBResult{OK: false, Driver: a.Driver, Query: a.Query, Err: err.Error(), Latency: time.Since(start)}
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return types.DBResult{OK: false, Driver: a.Driver, Query: a.Query, Err: err.Error(), Latency: time.Since(start)}
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return types.DBResult{OK: false, Driver: a.Driver, Query: a.Query, Err: err.Error(), Latency: time.Since(start)}
	}

	return types.DBResult{
		OK: true, Driver: a.Driver, Query: a.Query,
		Columns: cols, Rows: results,
		Latency: time.Since(start),
	}
}

func (e *DatabaseExecutor) doAssert(ctx context.Context, a types.DBAssertAction, start time.Time) types.ExecutorResult {
	queryResult := e.doQuery(ctx, types.DBQueryAction{
		Driver: a.Driver, DSN: a.DSN, Query: a.Query,
	}, start)
	dbResult, ok := queryResult.(types.DBResult)
	if !ok {
		return queryResult
	}
	if !dbResult.OK {
		return dbResult
	}

	// Evaluate simple assertions against the first row.
	passed := evaluateAssertion(a.Assertion, dbResult.Rows)
	return types.DBResult{
		OK: passed, Driver: a.Driver, Query: a.Query,
		Columns: dbResult.Columns, Rows: dbResult.Rows,
		AssertionPassed: passed,
		Latency:         time.Since(start),
	}
}

// evaluateAssertion evaluates simple expressions like "count == 0", "rows.length > 0".
func evaluateAssertion(assertion string, rows []map[string]any) bool {
	// Simple pattern: "field op value"
	// Supported: ==, !=, >, <, >=, <=
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		idx := indexOf(assertion, op)
		if idx < 0 {
			continue
		}
		field := trimSpace(assertion[:idx])
		valueStr := trimSpace(assertion[idx+len(op):])
		fieldVal := resolveField(field, rows)
		return compareValues(fieldVal, valueStr, op)
	}
	return false
}

func resolveField(field string, rows []map[string]any) string {
	if field == "rows.length" {
		return fmt.Sprintf("%d", len(rows))
	}
	if len(rows) > 0 {
		if v, ok := rows[0][field]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func compareValues(actual, expected, op string) bool {
	switch op {
	case "==":
		return actual == expected
	case "!=":
		return actual != expected
	case ">":
		return actual > expected
	case "<":
		return actual < expected
	case ">=":
		return actual >= expected
	case "<=":
		return actual <= expected
	}
	return false
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

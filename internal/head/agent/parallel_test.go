package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupParallelTest(t *testing.T) (*ReActLoop, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../../migrations")
	require.NoError(t, err)

	client := llm.NewMockClient(map[string]string{"default": `{"reasoning":"ok","action":{"type":"navigate","target":"/"}}`})
	driver := ai.NewDriver(client, ai.NewTokenBudget(500000, 50000))
	engine := NewRuleEngine("http://localhost", nil)
	httpExec := NewHTTPActionExecutor("http://localhost", zap.NewNop())
	config := DefaultReActConfig()
	loop := NewReActLoop(driver, s, engine, httpExec, config, zap.NewNop())
	return loop, s
}

func TestParallelExecutor_AllIndependent(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "parallel test", "project")
	require.NoError(t, err)

	// httptest server for action execution.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil)
	httpExec := NewHTTPActionExecutor(srv.URL, zap.NewNop())
	loop2 := NewReActLoop(loop.driver, s, engine, httpExec, DefaultReActConfig(), zap.NewNop())

	plan := &TestPlan{
		Goal:       "parallel test",
		ProjectURL: srv.URL,
		Cases: []TestCase{
			{ID: "tc-001", Name: "test 1", Target: "/a", Method: "GET", Expectation: "200"},
			{ID: "tc-002", Name: "test 2", Target: "/b", Method: "GET", Expectation: "200"},
			{ID: "tc-003", Name: "test 3", Target: "/c", Method: "GET", Expectation: "200"},
		},
	}

	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 1}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// All should pass (rule engine handles GET /path).
	for _, r := range results {
		assert.Equal(t, StepPassed, r.Status, "case %s should pass", r.TestCase.ID)
	}
}

func TestParallelExecutor_WithDependencies(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "dep test", "project")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil)
	httpExec := NewHTTPActionExecutor(srv.URL, zap.NewNop())
	_ = NewReActLoop(loop.driver, s, engine, httpExec, DefaultReActConfig(), zap.NewNop())

	// Track execution order.
	var order []string
	var orderMu int64

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := atomic.AddInt64(&orderMu, 1)
		order = append(order, r.URL.Path)
		// Ensure at least a small delay so order is observable.
		time.Sleep(time.Duration(idx) * 5 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv2.Close()

	engine2 := NewRuleEngine(srv2.URL, nil)
	httpExec2 := NewHTTPActionExecutor(srv2.URL, zap.NewNop())
	loop3 := NewReActLoop(loop.driver, s, engine2, httpExec2, DefaultReActConfig(), zap.NewNop())

	plan := &TestPlan{
		Goal:       "dep test",
		ProjectURL: srv2.URL,
		Cases: []TestCase{
			{ID: "tc-001", Name: "create user", Target: "/users", Method: "POST", Expectation: "201"},
			{ID: "tc-002", Name: "get user", Target: "/users/1", Method: "GET", Expectation: "200", DependsOn: "tc-001"},
			{ID: "tc-003", Name: "list users", Target: "/users", Method: "GET", Expectation: "200"},
		},
	}

	pe := NewParallelExecutor(loop3, ParallelConfig{MaxWorkers: 4}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify results exist for all cases.
	ids := make(map[string]StepStatus)
	for _, r := range results {
		ids[r.TestCase.ID] = r.Status
	}
	assert.Contains(t, ids, "tc-001")
	assert.Contains(t, ids, "tc-002")
	assert.Contains(t, ids, "tc-003")
}

func TestParallelExecutor_EmptyPlan(t *testing.T) {
	loop, _ := setupParallelTest(t)
	pe := NewParallelExecutor(loop, DefaultParallelConfig(), zap.NewNop())

	results, err := pe.ExecutePlan(context.Background(), &TestPlan{Goal: "empty"}, "session")
	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestParallelExecutor_ContextCancellation(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "cancel test", "project")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Slow response.
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil)
	httpExec := NewHTTPActionExecutor(srv.URL, zap.NewNop())
	loop2 := NewReActLoop(loop.driver, s, engine, httpExec, DefaultReActConfig(), zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	plan := &TestPlan{
		Goal:       "cancel test",
		ProjectURL: srv.URL,
		Cases: []TestCase{
			{ID: "tc-001", Name: "test 1", Target: "/a", Method: "GET"},
		},
	}

	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 1}, zap.NewNop())
	results, err := pe.ExecutePlan(ctx, plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	// Should be skipped or failed due to cancellation.
	assert.Contains(t, []StepStatus{StepSkipped, StepFailed}, results[0].Status)
}

func TestParallelExecutor_ConcurrencyLimit(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "concurrency", "project")
	require.NoError(t, err)

	var concurrent int64
	var maxConcurrent int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt64(&concurrent, 1)
		defer atomic.AddInt64(&concurrent, -1)

		// Track max concurrent.
		for {
			old := atomic.LoadInt64(&maxConcurrent)
			if current <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, current) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil)
	httpExec := NewHTTPActionExecutor(srv.URL, zap.NewNop())
	loop2 := NewReActLoop(loop.driver, s, engine, httpExec, DefaultReActConfig(), zap.NewNop())

	// 6 cases with MaxWorkers=2 — should never exceed 2 concurrent.
	cases := make([]TestCase, 6)
	for i := range cases {
		cases[i] = TestCase{
			ID:     fmtID(i + 1),
			Name:   "concurrent test",
			Target: string(rune('a' + i)),
			Method: "GET",
		}
	}

	plan := &TestPlan{Goal: "concurrency test", Cases: cases, ProjectURL: srv.URL}
	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 2}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 6)

	// Max concurrent should be <= 2.
	assert.LessOrEqual(t, atomic.LoadInt64(&maxConcurrent), int64(2))
}

func fmtID(i int) string {
	return string(rune('0' + i))
}

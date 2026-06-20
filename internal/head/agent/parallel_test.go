package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/store"
	"github.com/binoctal/cerberus/internal/types"
)

func setupParallelTest(t *testing.T) (*ReActLoop, *store.Store) {
	t.Helper()
	s, err := store.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	err = store.RunMigrations(ctx, s.DB(), "../../../migrations")
	require.NoError(t, err)

	steerJSON, _ := json.Marshal(SteerOutput{
		Reasoning: "ok",
		Envelope: types.ActionEnvelope{
			Type: types.ActionNavigate,
			Raw:  json.RawMessage(`{"url":"/"}`),
		},
	})
	client := llm.NewMockClient(map[string]string{"default": string(steerJSON)})
	driver := ai.NewDriver(client, ai.NewTokenBudget(500000, 50000))
	engine := NewRuleEngine("http://localhost", nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	config := DefaultReActConfig()
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop := NewReActLoopWithConfig(ReActLoopConfig{
		Driver:   driver,
		Store:    s,
		Engine:   engine,
		Executor: httpExec,
		Config:   config,
		Logger:   zap.NewNop(),
		Embedder: emb,
	})
	return loop, s
}

func TestParallelExecutor_AllIndependent(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "parallel test", "project")
	require.NoError(t, err)

	// httptest server for action execution.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

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
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	_ = NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

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

	engine2 := NewRuleEngine(srv2.URL, nil, ".")
	httpExec2 := BuildMultiExecutor(".", nil, zap.NewNop())
	loop3 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine2, Executor: httpExec2, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: embed.NewTrigramProvider(embed.DefaultDimension)})

	plan := &TestPlan{
		Goal:       "dep test",
		ProjectURL: srv2.URL,
		Cases: []TestCase{
			{ID: "tc-001", Name: "create user", Target: "/users", Method: "GET", Expectation: "200"},
			{ID: "tc-002", Name: "get user", Target: "/users/1", Method: "GET", Expectation: "200", DependsOn: Deps{"tc-001"}},
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

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

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

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

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

func TestParallelExecutor_CascadeSkip(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "cascade test", "project")
	require.NoError(t, err)

	// Server: /create and / return 500 (fail), everything else returns 200.
	// "/" must fail too — the mock LLM navigates there, so ReAct can't recover.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/create" || r.URL.Path == "/" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"internal"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	plan := &TestPlan{
		Goal:       "cascade test",
		ProjectURL: srv.URL,
		Cases: []TestCase{
			{ID: "tc-create", Name: "create resource", Target: "/create", Method: "GET", Expectation: "200"},
			{ID: "tc-get", Name: "get resource", Target: "/resource/1", Method: "GET", Expectation: "200", DependsOn: Deps{"tc-create"}},
			{ID: "tc-update", Name: "update resource", Target: "/resource/1", Method: "PUT", Expectation: "200", DependsOn: Deps{"tc-get"}},
			{ID: "tc-list", Name: "list all", Target: "/list", Method: "GET", Expectation: "200"},
		},
	}

	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 1}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 4)

	// Build result map.
	status := make(map[string]StepStatus)
	for _, r := range results {
		status[r.TestCase.ID] = r.Status
	}

	// tc-create should fail (500 response).
	assert.Equal(t, StepFailed, status["tc-create"], "parent should fail")

	// tc-get depends on tc-create → cascade skip.
	assert.Equal(t, StepSkipped, status["tc-get"], "child should be cascade-skipped")

	// tc-update depends on tc-get (which was skipped) → cascade skip.
	assert.Equal(t, StepSkipped, status["tc-update"], "grandchild should be cascade-skipped")

	// tc-list has no dependency → should pass.
	assert.Equal(t, StepPassed, status["tc-list"], "independent case should pass")
}

func TestParallelExecutor_CascadeSkip_ErrorMessage(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "cascade msg test", "project")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	plan := &TestPlan{
		Goal:       "cascade msg test",
		ProjectURL: srv.URL,
		Cases: []TestCase{
			{ID: "parent", Name: "parent", Target: "/fail", Method: "GET", Expectation: "200"},
			{ID: "child", Name: "child", Target: "/x", Method: "GET", Expectation: "200", DependsOn: Deps{"parent"}},
		},
	}

	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 1}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)

	// Child should have cascade skip error message.
	require.Len(t, results, 2)
	childResult := results[1]
	assert.Equal(t, StepSkipped, childResult.Status)
	require.Error(t, childResult.Error)
	assert.Contains(t, childResult.Error.Error(), "cascade skip")
	assert.Contains(t, childResult.Error.Error(), "parent")
}

func fmtID(i int) string {
	return string(rune('0' + i))
}

func TestDeps_UnmarshalJSON(t *testing.T) {
	t.Run("single string", func(t *testing.T) {
		var d Deps
		require.NoError(t, json.Unmarshal([]byte(`"tc-001"`), &d))
		assert.Equal(t, Deps{"tc-001"}, d)
	})

	t.Run("array of strings", func(t *testing.T) {
		var d Deps
		require.NoError(t, json.Unmarshal([]byte(`["tc-001","tc-002"]`), &d))
		assert.Equal(t, Deps{"tc-001", "tc-002"}, d)
	})

	t.Run("empty string", func(t *testing.T) {
		var d Deps
		require.NoError(t, json.Unmarshal([]byte(`""`), &d))
		assert.Nil(t, d)
	})

	t.Run("null", func(t *testing.T) {
		var d Deps
		require.NoError(t, json.Unmarshal([]byte(`null`), &d))
		assert.Nil(t, d)
	})
}

func TestDetectAndBreakCycles_NoCycle(t *testing.T) {
	graph := map[string][]string{
		"A": {},
		"B": {"A"},
		"C": {"A"},
	}
	clean := detectAndBreakCycles(graph, zap.NewNop())
	assert.Equal(t, []string{}, clean["A"])
	assert.Equal(t, []string{"A"}, clean["B"])
	assert.Equal(t, []string{"A"}, clean["C"])
}

func TestDetectAndBreakCycles_WithCycle(t *testing.T) {
	graph := map[string][]string{
		"A": {"C"},
		"B": {"A"},
		"C": {"B"},
	}
	clean := detectAndBreakCycles(graph, zap.NewNop())
	// Intra-cycle edges should be removed.
	assert.Empty(t, clean["A"])
	assert.Empty(t, clean["B"])
	assert.Empty(t, clean["C"])
}

func TestParallelExecutor_MultiDependency(t *testing.T) {
	loop, s := setupParallelTest(t)
	sess, err := s.CreateSession(context.Background(), "run", "multi-dep test", "project")
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	engine := NewRuleEngine(srv.URL, nil, ".")
	httpExec := BuildMultiExecutor(".", nil, zap.NewNop())
	emb := embed.NewTrigramProvider(embed.DefaultDimension)
	loop2 := NewReActLoopWithConfig(ReActLoopConfig{Driver: loop.driver, Store: s, Engine: engine, Executor: httpExec, Config: DefaultReActConfig(), Logger: zap.NewNop(), Embedder: emb})

	plan := &TestPlan{
		Goal:       "multi-dep test",
		ProjectURL: srv.URL,
		Cases: []TestCase{
			{ID: "tc-auth", Name: "authenticate", Target: "/a", Method: "GET", Expectation: "200"},
			{ID: "tc-prod", Name: "create product", Target: "/b", Method: "GET", Expectation: "200"},
			{ID: "tc-order", Name: "create order", Target: "/c", Method: "GET", Expectation: "200",
				DependsOn: Deps{"tc-auth", "tc-prod"}},
			{ID: "tc-free", Name: "independent", Target: "/d", Method: "GET", Expectation: "200"},
		},
	}

	pe := NewParallelExecutor(loop2, ParallelConfig{MaxWorkers: 1}, zap.NewNop())
	results, err := pe.ExecutePlan(context.Background(), plan, sess.ID)
	require.NoError(t, err)
	assert.Len(t, results, 4)

	status := make(map[string]StepStatus)
	for _, r := range results {
		status[r.TestCase.ID] = r.Status
	}
	assert.Equal(t, StepPassed, status["tc-auth"])
	assert.Equal(t, StepPassed, status["tc-prod"])
	assert.Equal(t, StepPassed, status["tc-order"])
	assert.Equal(t, StepPassed, status["tc-free"])
}

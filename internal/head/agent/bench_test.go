package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/types"
)

func BenchmarkRuleEngine_Match(b *testing.B) {
	services := []project.Service{{Name: "default", URL: "http://localhost:8080"}}
	engine := NewRuleEngine(services, nil, ".")

	cases := []TestCase{
		{ID: "api", Target: "/api/v1/users", Method: "GET"},
		{ID: "navigate", Target: "/page", Action: "navigate"},
		{ID: "url", Target: "https://example.com/api", Method: "GET"},
		{ID: "process", Target: "go test", Action: "process_exec"},
		{ID: "file", Target: "/tmp/test.txt", Action: "file_read"},
		{ID: "miss", Target: "unknown-target", Action: "custom"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tc := cases[i%len(cases)]
		engine.Match(tc)
	}
}

func BenchmarkRuleEngine_MatchParallel(b *testing.B) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "http://localhost:8080"}}, nil, ".")
	tc := TestCase{ID: "api", Target: "/api/v1/users", Method: "GET"}

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			engine.Match(tc)
		}
	})
}

func BenchmarkRuleEngine_Stats(b *testing.B) {
	engine := NewRuleEngine([]project.Service{{Name: "default", URL: "http://localhost:8080"}}, nil, ".")
	tc := TestCase{ID: "api", Target: "/api", Method: "GET"}
	for i := 0; i < 1000; i++ {
		engine.Match(tc)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		engine.Stats()
	}
}

func BenchmarkHTTPExecutor_Execute(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	exec := NewHTTPExecutor(zap.NewNop())
	action := types.HTTPAction{Method: "GET", URL: srv.URL + "/api/v1/users"}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exec.Execute(ctx, action)
	}
}

func BenchmarkBuildMultiExecutor(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildMultiExecutor(".", nil, nil, zap.NewNop())
	}
}

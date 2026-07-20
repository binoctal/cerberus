package agent

import (
	"testing"

	"github.com/binoctal/cerberus/internal/project"
)

func TestExtractTypePath(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
		want string
		ok   bool
	}{
		{name: "top-level type", data: `{"type":"permission:response"}`, path: "type", want: "permission:response", ok: true},
		{name: "default empty path = top-level type", data: `{"type":"x"}`, path: "", want: "x", ok: true},
		{name: "nested path", data: `{"data":{"event":"ping"}}`, path: "data.event", want: "ping", ok: true},
		{name: "missing path", data: `{"type":"x"}`, path: "data.event", want: "", ok: false},
		{name: "non-string leaf no match", data: `{"type":123}`, path: "type", want: "", ok: false},
		{name: "non-json no match", data: `not json`, path: "type", want: "", ok: false},
		{name: "json array no match", data: `[1,2,3]`, path: "type", want: "", ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractTypePath([]byte(tc.data), tc.path)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got (%q,%v) want (%q,%v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestBuildWSProtocolIndex(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{
			Name: "rt", URL: "http://localhost:8787",
			Protocol: &project.Protocol{TypePath: "data.event"},
		}},
		Actors: []project.Actor{{Name: "web", Credentials: project.CredentialRef{RawToken: "JWT"}}},
	}
	idx := BuildWSProtocolIndex(cfg)
	if idx == nil {
		t.Fatal("index is nil")
	}
	if p, ok := idx.ByHost["localhost:8787"]; !ok || p.TypePath != "data.event" {
		t.Fatalf("ByHost = %+v", idx.ByHost)
	}
	if idx.ActorTokens["web"] != "JWT" {
		t.Fatalf("ActorTokens = %+v", idx.ActorTokens)
	}
}

func TestBuildWSProtocolIndexNilWhenNoProtocols(t *testing.T) {
	cfg := &project.Config{Services: []project.Service{{Name: "x", URL: "http://x"}}}
	if idx := BuildWSProtocolIndex(cfg); idx != nil {
		t.Fatalf("want nil index when no protocols, got %+v", idx)
	}
}

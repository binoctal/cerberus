package agent

import (
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/binoctal/cerberus/internal/project"
	"github.com/stretchr/testify/require"
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

func TestExtractPath(t *testing.T) {
	cases := []struct {
		name string
		data string
		path string
		want any
		ok   bool
	}{
		{"empty path reads type", `{"type":"devices:sync"}`, "", "devices:sync", true},
		{"nested object leaf", `{"payload":{"approved":true}}`, "payload.approved", true, true},
		{"string leaf", `{"type":"x","role":"admin"}`, "role", "admin", true},
		{"number leaf decodes float64", `{"n":5}`, "n", float64(5), true},
		{"absent key", `{"a":1}`, "b", nil, false},
		{"non-object mid-path", `{"a":"str"}`, "a.b", nil, false},
		{"present null", `{"v":null}`, "v", nil, true},
		{"not json", `not-json`, "a", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractPath([]byte(tc.data), tc.path)
			if ok != tc.ok {
				t.Fatalf("extractPath(%q) ok = %v, want %v (got %v)", tc.path, ok, tc.ok, got)
			}
			if ok && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("extractPath(%q) = %#v, want %#v", tc.path, got, tc.want)
			}
		})
	}
}

func TestMatchType(t *testing.T) {
	jsonMsg := []byte(`{"type":"go"}`)
	binWant := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff})
	cases := []struct {
		name     string
		framing  string
		data     []byte
		want     string
		typePath string
		expect   bool
	}{
		{"json match", "json", jsonMsg, "go", "type", true},
		{"json no match", "json", jsonMsg, "other", "type", false},
		{"empty framing = json", "", jsonMsg, "go", "type", true},
		{"text exact match", "text", []byte("READY"), "READY", "", true},
		{"text no match", "text", []byte("READY"), "PENDING", "", false},
		{"binary match", "binary", []byte{0x00, 0xff}, binWant, "", true},
		{"binary no match", "binary", []byte{0x00, 0xff}, base64.StdEncoding.EncodeToString([]byte{0x01}), "", false},
		{"binary invalid want no match", "binary", []byte{0x00}, "@@@@", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchType(tc.framing, tc.data, tc.want, tc.typePath); got != tc.expect {
				t.Fatalf("matchType(%q,...) = %v, want %v", tc.framing, got, tc.expect)
			}
		})
	}
}

func TestMatchAnyType(t *testing.T) {
	const frame = `{"type":"session:output-batch","payload":{"lines":["hi"]}}`
	tests := []struct {
		name  string
		types []string
		want  bool
	}{
		{"primary match", []string{"session:output-batch"}, true},
		{"alias match primary absent", []string{"session:output", "session:output-batch"}, true},
		{"alias match order independent", []string{"session:output-batch", "session:output"}, true},
		{"no match", []string{"session:output", "device:ack"}, false},
		{"empty set never matches", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, matchAnyType("", []byte(frame), tc.types, "type"))
		})
	}
	// Backwards-compat: matchAnyType over a 1-element set == matchType.
	require.Equal(t, matchType("", []byte(frame), "session:output-batch", "type"),
		matchAnyType("", []byte(frame), []string{"session:output-batch"}, "type"))
}

func TestFrameForResult(t *testing.T) {
	if got := frameForResult("binary", []byte{0x00, 0xff}); got != base64.StdEncoding.EncodeToString([]byte{0x00, 0xff}) {
		t.Fatalf("binary frameForResult = %q", got)
	}
	if got := frameForResult("text", []byte("hi")); got != "hi" {
		t.Fatalf("text frameForResult = %q", got)
	}
	if got := frameForResult("", []byte("hi")); got != "hi" {
		t.Fatalf("empty framing frameForResult = %q", got)
	}
}

func TestFramingOf(t *testing.T) {
	if got := framingOf(&wsEntry{}); got != "" {
		t.Fatalf("nil protocol framing = %q, want empty", got)
	}
	if got := framingOf(&wsEntry{protocol: &project.Protocol{Framing: "binary"}}); got != "binary" {
		t.Fatalf("framing = %q, want binary", got)
	}
}

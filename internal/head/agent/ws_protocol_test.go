package agent

import "testing"

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

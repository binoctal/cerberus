package agent

import (
	"encoding/base64"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

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

// TestBuildWSProtocolIndexActorPathParams proves F3: an actor with captured
// Credentials.PathParams is reflected into ActorPathParams so doConnect can
// resolve {param} placeholders in the dial URL. An actor without path params
// is omitted entirely (no empty map stashed), preserving legacy behavior.
func TestBuildWSProtocolIndexActorPathParams(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{
			Name: "rt", URL: "http://localhost:8787",
			Protocol: &project.Protocol{TypePath: "data.event"},
		}},
		Actors: []project.Actor{
			{Name: "web", Credentials: project.CredentialRef{RawToken: "JWT"}},
			{Name: "bridge", Credentials: project.CredentialRef{
				RawToken:   "BRIDGE-JWT",
				PathParams: map[string]string{"userId": "user_1", "tenant": "acme"},
			}},
		},
	}
	idx := BuildWSProtocolIndex(cfg)
	if idx == nil {
		t.Fatal("index is nil")
	}
	if got, want := idx.ActorPathParams["bridge"]["userId"], "user_1"; got != want {
		t.Fatalf("ActorPathParams[bridge][userId] = %q, want %q (full: %+v)", got, want, idx.ActorPathParams)
	}
	if got, want := idx.ActorPathParams["bridge"]["tenant"], "acme"; got != want {
		t.Fatalf("ActorPathParams[bridge][tenant] = %q, want %q", got, want)
	}
	// An actor without path params must NOT appear (no empty-map stash) so the
	// "no path params" legacy path stays a clean nil-map lookup.
	if _, ok := idx.ActorPathParams["web"]; ok {
		t.Fatalf("ActorPathParams[web] should be absent, got %+v", idx.ActorPathParams["web"])
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
	// Binary framing: an invalid-base64 alias never matches and does not panic
	// (OR safe — matchType returns false on decode error).
	require.False(t, matchAnyType("binary", []byte{0x00, 0x01}, []string{"@@@@"}, ""))
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

func TestBuildWSProtocolIndex_ActorHTTPTokens(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{
			Name:     "realtime",
			URL:      "http://h/ws/{userId}",
			Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}},
		}},
		Actors: []project.Actor{{
			Name:        "web-actor",
			Credentials: project.CredentialRef{RawHTTPToken: "JWT-9", RawToken: "demo"},
		}},
	}
	idx := BuildWSProtocolIndex(cfg)
	if idx == nil {
		t.Fatal("expected non-nil index")
	}
	if idx.ActorHTTPTokens["web-actor"] != "JWT-9" {
		t.Fatalf("ActorHTTPTokens[web-actor] = %q, want JWT-9", idx.ActorHTTPTokens["web-actor"])
	}
	if idx.ActorTokens["web-actor"] != "demo" {
		t.Fatalf("ActorTokens[web-actor] = %q, want demo (unchanged)", idx.ActorTokens["web-actor"])
	}
}

// TestBuildWSProtocolIndex_StaticToken proves the static-Token fallback: an
// actor with only Credentials.Token (no Auth flow / RawToken) still yields an
// ActorTokens entry, a flow-resolved RawToken wins over the static Token, and
// an actor with neither leaves ActorTokens untouched (backwards-compat).
func TestBuildWSProtocolIndex_StaticToken(t *testing.T) {
	cfg := &project.Config{
		Services: []project.Service{{Name: "s", URL: "ws://h/ws", Protocol: &project.Protocol{Roles: map[string]*project.ProtocolRole{"web": {}}}}},
		Actors: []project.Actor{
			{Name: "static", Credentials: project.CredentialRef{Token: "demo_token"}},
			{Name: "flowed", Credentials: project.CredentialRef{Token: "FALLBACK", RawToken: "FLOW"}},
			{Name: "none", Credentials: project.CredentialRef{}},
		},
	}
	idx := BuildWSProtocolIndex(cfg)
	require.Equal(t, "demo_token", idx.ActorTokens["static"], "static Token used when no RawToken")
	require.Equal(t, "FLOW", idx.ActorTokens["flowed"], "RawToken wins over static Token")
	_, ok := idx.ActorTokens["none"]
	require.False(t, ok, "no token + no flow ⇒ no ActorTokens entry")
}

// TestExpandBatch covers the pump's batch decomposition: a declared json batch
// frame expands into N synthetic item frames (ordered, retyped); everything else
// (no protocol, binary, non-json framing, non-batch type, missing/empty/non-array
// path) passes the original frame through unchanged.
func TestExpandBatch(t *testing.T) {
	batchProto := &project.Protocol{
		TypePath: "type",
		Batches: map[string]*project.ProtocolBatch{
			"session:output-batch": {ItemType: "session:output", ItemsPath: "payload.lines"},
		},
	}
	batchFrame := []byte(`{"type":"session:output-batch","payload":{"lines":["a","b","c"]}}`)
	objBatch := &project.Protocol{TypePath: "type", Batches: map[string]*project.ProtocolBatch{
		"ev-batch": {ItemType: "ev", ItemsPath: "payload.events"},
	}}
	objFrame := []byte(`{"type":"ev-batch","payload":{"events":[{"id":1},{"id":2}]}}`)

	cases := []struct {
		name       string
		proto      *project.Protocol
		data       []byte
		binary     bool
		wantFrames int       // number of frames returned
		wantType   string    // routing type of the (first) returned frame
		wantItems  []string  // for the array-of-strings case, the payload of each frame in order
		wantObjIDs []float64 // for the array-of-objects case, each item's payload.id
	}{
		{"batch expands to N items", batchProto, batchFrame, false, 3, "session:output", []string{"a", "b", "c"}, nil},
		{"batch of objects expands", objBatch, objFrame, false, 2, "ev", nil, []float64{1, 2}},
		{"no protocol passes through", nil, batchFrame, false, 1, "session:output-batch", nil, nil},
		{"binary passes through", batchProto, batchFrame, true, 1, "", nil, nil},
		{"non-batch type passes through", batchProto, []byte(`{"type":"other"}`), false, 1, "other", nil, nil},
		{"empty array passes through", batchProto, []byte(`{"type":"session:output-batch","payload":{"lines":[]}}`), false, 1, "session:output-batch", nil, nil},
		{"missing array path passes through", batchProto, []byte(`{"type":"session:output-batch","payload":{}}`), false, 1, "session:output-batch", nil, nil},
		{"non-json passes through", batchProto, []byte("not json"), false, 1, "", nil, nil},
		{"text framing passes through", &project.Protocol{Framing: "text", Batches: batchProto.Batches}, batchFrame, false, 1, "", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandBatch(c.proto, c.data, c.binary)
			require.Len(t, got, c.wantFrames, "frame count")
			if c.wantType != "" {
				rt, ok := extractTypePath(got[0].data, "type")
				require.True(t, ok, "first frame has a type")
				require.Equal(t, c.wantType, rt, "first frame type")
			}
			if c.wantItems != nil {
				var items []string
				for _, m := range got {
					v, ok := extractPath(m.data, "payload")
					require.True(t, ok)
					items = append(items, v.(string))
				}
				require.Equal(t, c.wantItems, items, "expanded item order/contents")
			}
			if c.wantObjIDs != nil {
				var ids []float64
				for _, m := range got {
					v, ok := extractPath(m.data, "payload.id")
					require.True(t, ok)
					ids = append(ids, v.(float64))
				}
				require.Equal(t, c.wantObjIDs, ids, "expanded object item ids")
			}
		})
	}
}

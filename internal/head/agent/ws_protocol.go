package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"sync"

	"github.com/binoctal/cerberus/internal/project"
)

// extractPath walks a dotted path through a JSON message and returns the leaf
// value. An empty path returns the top-level "type" field's value (M0 routing
// semantics, shared with extractTypePath). Returns (value, true) when the path
// resolves to a present leaf — including a JSON null, which is a present nil —
// and (nil, false) if the message is not a JSON object, the path traverses a
// non-object, or the leaf key is absent.
func extractPath(data []byte, path string) (any, bool) {
	if path == "" {
		path = "type"
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, false
	}
	cur := any(obj)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := m[key]
		if !exists {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// extractTypePath returns the routing key at the dotted path within a JSON
// message as a string. Empty path means top-level "type" (M0 behavior). Returns
// ("", false) if the message is not a JSON object, the path is absent, or the
// leaf is not a string — so the M0 fallback reproduces messageType semantics
// exactly (a non-string type field does not match).
func extractTypePath(data []byte, path string) (string, bool) {
	v, ok := extractPath(data, path)
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// extractArray walks a dotted path to a JSON array and returns its elements.
// Returns (nil, false) if the message is not a JSON object, the path is absent,
// or the leaf is not a JSON array. Used by batch decomposition to find the items
// to expand.
func extractArray(data []byte, path string) ([]any, bool) {
	v, ok := extractPath(data, path)
	if !ok {
		return nil, false
	}
	arr, ok := v.([]any)
	return arr, ok
}

// framingOf returns the effective wire framing for a connection. Empty (no
// protocol, or a protocol with no framing) means json — the M0/M1 default.
func framingOf(entry *wsEntry) string {
	if entry.protocol != nil {
		return entry.protocol.Framing
	}
	return ""
}

// matchType reports whether a received frame satisfies the awaited type under
// the connection's framing. json routes by type_path; text matches the whole
// frame text exactly; binary matches the whole frame bytes exactly (want is
// base64). A binary want that is not valid base64 never matches.
func matchType(framing string, data []byte, want, typePath string) bool {
	switch framing {
	case "text":
		return string(data) == want
	case "binary":
		got, err := base64.StdEncoding.DecodeString(want)
		if err != nil {
			return false
		}
		return bytes.Equal(got, data)
	default: // "" or "json"
		t, ok := extractTypePath(data, typePath)
		return ok && t == want
	}
}

// matchAnyType reports whether a received frame matches ANY of types under the
// connection's framing. It is matchType over a set; the empty set never matches.
// Used by ws_receive with aliases (e.g. session:output vs session:output-batch).
func matchAnyType(framing string, data []byte, types []string, typePath string) bool {
	for _, t := range types {
		if matchType(framing, data, t, typePath) {
			return true
		}
	}
	return false
}

// frameForResult renders received bytes for a WSResult string field under the
// connection's framing. binary frames are base64-encoded; text/json frames are
// the raw UTF-8 text.
func frameForResult(framing string, data []byte) string {
	if framing == "binary" {
		return base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}

// WSProtocolIndex gives the WS executor the per-host protocol declaration and
// the resolved raw credential tokens for actors referenced by those protocols.
// A nil index means "no service declares a protocol" → M0 behavior everywhere.
type WSProtocolIndex struct {
	ByHost          map[string]*project.Protocol // host (url.Host) -> protocol
	ActorTokens     map[string]string            // actor name -> cached raw token
	ActorPathParams map[string]map[string]string // actor -> {url-param: value} (F3)
	// ActorHTTPTokens maps actor name -> the HTTP credential captured by an
	// optional http_login (distinct from ActorTokens, the WS credential). Read
	// by the Steps runner to inject http_request Authorization headers, and
	// rotated mid-run by the session's token refresher (SUT access tokens
	// expire in 15 minutes while sweeps run for hours).
	ActorHTTPTokens map[string]string

	httpTokenMu sync.RWMutex
}

// SetActorHTTPToken rotates an actor's HTTP credential in the live index.
// An empty token is ignored — a failed re-login must not blank a working
// token. Safe for concurrent use with resolving http_request steps.
func (idx *WSProtocolIndex) SetActorHTTPToken(actor, token string) {
	if token == "" {
		return
	}
	idx.httpTokenMu.Lock()
	idx.ActorHTTPTokens[actor] = token
	idx.httpTokenMu.Unlock()
}

// ActorHTTPToken returns the actor's current HTTP credential ("" when none).
func (idx *WSProtocolIndex) ActorHTTPToken(actor string) string {
	idx.httpTokenMu.RLock()
	defer idx.httpTokenMu.RUnlock()
	return idx.ActorHTTPTokens[actor]
}

// BuildWSProtocolIndex builds the index from config. Returns nil when no
// service declares a protocol, so the executor can short-circuit to M0
// behavior without checking every dial.
func BuildWSProtocolIndex(cfg *project.Config) *WSProtocolIndex {
	var idx *WSProtocolIndex
	for _, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		u, err := url.Parse(svc.URL)
		if err != nil {
			continue
		}
		if idx == nil {
			idx = &WSProtocolIndex{
				ByHost:          make(map[string]*project.Protocol),
				ActorTokens:     make(map[string]string),
				ActorPathParams: make(map[string]map[string]string),
				ActorHTTPTokens: make(map[string]string),
			}
		}
		idx.ByHost[u.Host] = svc.Protocol
	}
	if idx == nil {
		return nil
	}
	for _, a := range cfg.Actors {
		token := a.Credentials.RawToken
		if token == "" {
			token = a.Credentials.Token // static fallback (no flow / flow failed)
		}
		if token != "" {
			idx.ActorTokens[a.Name] = token
		}
		// F3: url-param -> captured value, used to resolve {param} placeholders
		// in the dial URL at connect time. Only stashed when non-empty so a
		// legacy config (no auth flow / no path_params) leaves the index untouched.
		if len(a.Credentials.PathParams) > 0 {
			// Shallow copy so the index owns its map (defensive against the
			// actor's Credentials mutating after indexing).
			m := make(map[string]string, len(a.Credentials.PathParams))
			for k, v := range a.Credentials.PathParams {
				m[k] = v
			}
			idx.ActorPathParams[a.Name] = m
		}
		// HTTP credential from the optional http_login; only stashed when
		// non-empty so a legacy config (no http_login) leaves the slot absent.
		if a.Credentials.RawHTTPToken != "" {
			idx.ActorHTTPTokens[a.Name] = a.Credentials.RawHTTPToken
		}
	}
	return idx
}

package agent

import (
	"encoding/json"
	"net/url"
	"strings"

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

// WSProtocolIndex gives the WS executor the per-host protocol declaration and
// the resolved raw credential tokens for actors referenced by those protocols.
// A nil index means "no service declares a protocol" → M0 behavior everywhere.
type WSProtocolIndex struct {
	ByHost      map[string]*project.Protocol // host (url.Host) -> protocol
	ActorTokens map[string]string            // actor name -> cached raw token
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
				ByHost:      make(map[string]*project.Protocol),
				ActorTokens: make(map[string]string),
			}
		}
		idx.ByHost[u.Host] = svc.Protocol
	}
	if idx == nil {
		return nil
	}
	for _, a := range cfg.Actors {
		if a.Credentials.RawToken != "" {
			idx.ActorTokens[a.Name] = a.Credentials.RawToken
		}
	}
	return idx
}

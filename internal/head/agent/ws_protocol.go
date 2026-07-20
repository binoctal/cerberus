package agent

import (
	"encoding/json"
	"strings"
)

// extractTypePath returns the routing key at the dotted path within a JSON
// message. An empty path means top-level "type" (M0 behavior). Returns
// ("", false) if the message is not a JSON object, the path is absent, or the
// leaf is not a string — so the M0 fallback path reproduces messageType
// semantics exactly (a non-string type field does not match).
func extractTypePath(data []byte, path string) (string, bool) {
	if path == "" {
		path = "type"
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err != nil {
		return "", false
	}
	cur := any(obj)
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", false
		}
		next, exists := m[key]
		if !exists {
			return "", false
		}
		cur = next
	}
	s, ok := cur.(string)
	return s, ok
}

package llm

// Field helpers extract typed values from a ToolCall.Input map
// (map[string]any from provider JSON). They are shared by the Scout and Agent
// assemblers so the two heads do not duplicate identical coercion logic.
//
// Each helper returns the zero value when the key is absent or the value is
// not the expected Go type, matching the JSON-unmarshal convention used by
// every action struct in internal/types.

// StrField returns the string at k, or "" if missing/not a string.
func StrField(c ToolCall, k string) string {
	if v, ok := c.Input[k].(string); ok {
		return v
	}
	return ""
}

// IntField returns the int at k, accepting either float64 (the default JSON
// number shape) or int, or 0 if missing/unsupported.
func IntField(c ToolCall, k string) int {
	switch v := c.Input[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// NumField returns the float64 at k, or 0 if missing/not a number.
func NumField(c ToolCall, k string) float64 {
	if v, ok := c.Input[k].(float64); ok {
		return v
	}
	return 0
}

// StrSliceField returns the []string at k. Each element of the JSON array
// must decode to a string; non-string elements are dropped (matching the
// schema-enforced arrays emitted by the provider).
func StrSliceField(c ToolCall, k string) []string {
	arr, ok := c.Input[k].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// MapField returns the map[string]any at k, or nil if missing/not an object.
func MapField(c ToolCall, k string) map[string]any {
	if m, ok := c.Input[k].(map[string]any); ok {
		return m
	}
	return nil
}

// MapStringStringField returns the map[string]string at k. Each value of the
// JSON object must decode to a string; non-string values are dropped. Used for
// action fields like HTTPAction.Headers and ProcessExecAction.Env whose Go
// types are map[string]string.
func MapStringStringField(c ToolCall, k string) map[string]string {
	m, ok := c.Input[k].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(m))
	for key, v := range m {
		if s, ok := v.(string); ok {
			out[key] = s
		}
	}
	return out
}

// AnySliceField returns the []any at k, or nil if missing/not an array. Used
// for action fields whose Go types are []any (e.g. BrowserEvalAction.Args).
func AnySliceField(c ToolCall, k string) []any {
	if arr, ok := c.Input[k].([]any); ok {
		return arr
	}
	return nil
}

// BoolField returns the bool at k, or false if missing/not a bool.
func BoolField(c ToolCall, k string) bool {
	if v, ok := c.Input[k].(bool); ok {
		return v
	}
	return false
}

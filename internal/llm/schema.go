package llm

// Object schema helpers shared by the Scout, Agent, and Examiner tool
// surfaces. They produce provider-facing JSON Schema fragments matching
// the shape previously duplicated in scout/tools.go and agent/tools.go.

// ObjSchema wraps an object schema with required + properties. A nil
// required slice means every property is optional (used by tools that have
// no required field, e.g. wait/skip); in that case the "required" key is
// omitted entirely so the serialized schema matches what providers expect.
func ObjSchema(required []any, props map[string]any) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if required != nil {
		s["required"] = required
	}
	return s
}

// StrArrSchema returns a schema for a free-form string array.
func StrArrSchema() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

// EnumArrSchema returns a schema for a string array whose items are
// constrained to one of the provided enum values.
func EnumArrSchema(vals ...string) map[string]any {
	cs := make([]any, len(vals))
	for i, v := range vals {
		cs[i] = v
	}
	return map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": cs}}
}

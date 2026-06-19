package architecture

// Responsibility represents a code responsibility
type Responsibility struct {
	Name     string
	Keywords []string
	Examples []string
}

// Common responsibility patterns
var responsibilityPatterns = []Responsibility{
	{"parsing", []string{"parse", "read", "decode", "unmarshal", "extract"}, []string{}},
	{"validation", []string{"validate", "check", "verify", "ensure", "confirm"}, []string{}},
	{"persistence", []string{"save", "persist", "store", "write", "insert", "update", "delete"}, []string{}},
	{"calculation", []string{"calculate", "compute", "evaluate", "process"}, []string{}},
	{"rendering", []string{"render", "display", "show", "format", "print"}, []string{}},
	{"network", []string{"fetch", "request", "send", "receive", "connect"}, []string{}},
	{"configuration", []string{"config", "setting", "option", "parameter"}, []string{}},
	{"logging", []string{"log", "debug", "trace", "info", "warn", "error"}, []string{}},
	{"testing", []string{"test", "mock", "stub", "fixture"}, []string{}},
	{"caching", []string{"cache", "buffer", "store"}, []string{}},
}

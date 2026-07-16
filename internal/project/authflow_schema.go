package project

// AuthFlow declaratively obtains a dynamic credential once per session and
// injects it as a static header. When Auth is nil on an Actor, cerberus keeps
// the existing static-header behavior unchanged.
type AuthFlow struct {
	Login     AuthLogin `yaml:"login"`
	TokenFrom string    `yaml:"token_from"` // dot-path into response JSON (e.g. "token", "data.accessToken")
	InjectAs  string    `yaml:"inject_as"`  // header template, "{token}" substituted (e.g. "Authorization: Bearer {token}")
}

// AuthLogin describes the single login HTTP request.
type AuthLogin struct {
	Method  string            `yaml:"method"`            // e.g. POST
	Path    string            `yaml:"path"`              // relative to service.URL, or absolute
	Body    map[string]string `yaml:"body,omitempty"`    // values may reference "{email}"/"{password}" from credentials
	Headers map[string]string `yaml:"headers,omitempty"` // optional static headers on the login request
}

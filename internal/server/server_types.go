package server

// createSessionRequest represents a request to create a new test session.
type createSessionRequest struct {
	Mode  string `json:"mode"`  // "run" or "verify", default "run"
	Goal  string `json:"goal"`  // required
	URL   string `json:"url"`   // override project URL
	Model string `json:"model"` // override LLM model
}

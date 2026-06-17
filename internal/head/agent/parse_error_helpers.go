package agent

// checkParseOutputError checks for "parse output" pattern
func checkParseOutputError(msg string) bool {
	return contains(msg, "parse output")
}

// checkJSONUnmarshalError checks for JSON unmarshaling errors
func checkJSONUnmarshalError(msg string) bool {
	return contains(msg, "unmarshal") ||
		contains(msg, "invalid character") ||
		contains(msg, "unexpected end")
}

// checkJSONSyntaxError checks for JSON syntax/format errors
func checkJSONSyntaxError(msg string) bool {
	if !contains(msg, "json") {
		return false
	}
	return contains(msg, "syntax") ||
		contains(msg, "error") ||
		contains(msg, "format") ||
		contains(msg, "invalid")
}

package ai

// tryGetCachedResponse attempts to retrieve a cached response for the given prompt.
// Returns (content, true) if cached, ("", false) if not cached or cache is disabled.
// If cached and schema is non-nil, also parses the content into schema.
func tryGetCachedResponse(cache *ResponseCache, prompt string, schema any) (string, bool, error) {
	if cache == nil {
		return "", false, nil
	}

	content, _, ok := cache.Get(prompt)
	if !ok {
		return "", false, nil
	}

	// If schema provided, parse cached content
	if schema != nil {
		if err := ParseStructuredOutput(content, schema); err != nil {
			return "", true, err
		}
	}

	return content, true, nil
}

// cacheResponse stores a response in the cache if caching is enabled.
// Safe to call with nil cache (no-op).
func cacheResponse(cache *ResponseCache, prompt string, content string, tokens int) {
	if cache == nil {
		return
	}
	cache.Set(prompt, content, TokenUsage{TotalTokens: tokens})
}

package ai

import (
	"encoding/json"
	"regexp"
)

var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

// tryDirectJSON attempts to parse input as direct JSON
func tryDirectJSON(input string, target any) bool {
	return json.Unmarshal([]byte(input), target) == nil
}

// tryMarkdownJSON attempts to extract JSON from markdown code blocks
func tryMarkdownJSON(input string, target any) bool {
	match := jsonBlockRE.FindStringSubmatch(input)
	if match != nil {
		return json.Unmarshal([]byte(match[1]), target) == nil
	}
	return false
}

// jsonBounds represents the start and end indices of a JSON object
type jsonBounds struct {
	start int
	end   int
}

// findJSONObjectBounds finds the outermost JSON object boundaries
func findJSONObjectBounds(input string) *jsonBounds {
	start := -1
	end := -1
	braceDepth := 0

	for i, c := range input {
		if c == '{' {
			if start == -1 {
				start = i
			}
			braceDepth++
		}
		if c == '}' && braceDepth > 0 {
			braceDepth--
			if braceDepth == 0 {
				end = i
				break
			}
		}
	}

	if start != -1 && end > start {
		return &jsonBounds{start: start, end: end}
	}
	return nil
}

// tryExtractJSON attempts to extract JSON from input by finding matching braces
func tryExtractJSON(input string, target any) bool {
	bounds := findJSONObjectBounds(input)
	if bounds != nil {
		return json.Unmarshal([]byte(input[bounds.start:bounds.end+1]), target) == nil
	}
	return false
}

package ai

import (
	"encoding/json"
	"regexp"
	"strings"
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

// escapeControlInStrings escapes literal control characters that appear inside
// JSON string values. JSON forbids raw 0x00-0x1F inside strings, but some LLMs
// (notably GLM) emit multi-line reasoning with raw newlines, producing invalid
// JSON. This scans tracking string boundaries (honoring backslash escapes) and
// escapes in-string \n, \r, \t. Characters outside strings are left untouched.
func escapeControlInStrings(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if inString && c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString {
			switch c {
			case '\n':
				b.WriteString(`\n`)
				continue
			case '\r':
				b.WriteString(`\r`)
				continue
			case '\t':
				b.WriteString(`\t`)
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

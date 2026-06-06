package ai

import (
	"encoding/json"
	"regexp"
)

var jsonBlockRE = regexp.MustCompile("(?s)```(?:json)?\\s*\\n(.*?)\\n```")

func ParseStructuredOutput(input string, target any) error {
	if err := json.Unmarshal([]byte(input), target); err == nil {
		return nil
	}

	match := jsonBlockRE.FindStringSubmatch(input)
	if match != nil {
		return json.Unmarshal([]byte(match[1]), target)
	}

	start := -1
	end := -1
	for i, c := range input {
		if c == '{' && start == -1 {
			start = i
		}
		if c == '}' {
			end = i
		}
	}
	if start != -1 && end > start {
		return json.Unmarshal([]byte(input[start:end+1]), target)
	}

	return json.Unmarshal([]byte(input), target)
}

package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// caseParamRe matches {{case.<name>}} placeholders in step URL/Body/Message.
var caseParamRe = regexp.MustCompile(`\{\{case\.([A-Za-z0-9_]+)\}\}`)

// substituteCaseParams rewrites {{case.<name>}} placeholders from params.
// Leftover placeholders stay verbatim — the downstream request failing on a
// literal {{case.x}} is a clearer failure than a silent empty string.
func substituteCaseParams(s TestStep, params map[string]string) TestStep {
	replace := func(in string) string {
		return caseParamRe.ReplaceAllStringFunc(in, func(m string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(m, "{{case."), "}}")
			if v, ok := params[name]; ok {
				return v
			}
			return m
		})
	}
	s.URL, s.Body, s.Message = replace(s.URL), replace(s.Body), replace(s.Message)
	return s
}

// captureFromHTTPBody walks dot-paths into the JSON body and stringifies
// the scalar leaf. Missing paths are hard errors (clear failure over a
// silently-wrong later request — same policy as resolveURLParams).
func captureFromHTTPBody(body string, capture map[string]string) (map[string]string, error) {
	if len(capture) == 0 {
		return nil, nil
	}
	var root any
	if err := json.Unmarshal([]byte(body), &root); err != nil {
		return nil, fmt.Errorf("capture: response body is not JSON: %w", err)
	}
	out := make(map[string]string, len(capture))
	for path, name := range capture {
		cur := root
		var leaf = cur
		ok := true
		for _, seg := range strings.Split(path, ".") {
			m, isMap := cur.(map[string]any)
			if !isMap {
				ok = false
				break
			}
			leaf, ok = m[seg]
			if !ok {
				break
			}
			cur = leaf
		}
		if !ok {
			return nil, fmt.Errorf("capture: path %q not found in response", path)
		}
		switch leaf.(type) {
		case map[string]any, []any:
			return nil, fmt.Errorf("capture: path %q is not a scalar", path)
		}
		out[name] = fmt.Sprint(leaf)
	}
	return out, nil
}

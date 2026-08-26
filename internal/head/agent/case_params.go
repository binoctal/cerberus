package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// caseParamRe matches {{case.<name>}} placeholders in step URL/Body/Message.
var caseParamRe = regexp.MustCompile(`\{\{case\.([A-Za-z0-9_]+)\}\}`)

// substituteCaseParams rewrites {{case.<name>}} placeholders from params.
// Leftover placeholders stay verbatim — the downstream request failing on a
// literal {{case.x}} is a clearer failure than a silent empty string.
// Target is substituted too: browser steps carry their Playwright selector
// there, and protocol-coupled UI assertions template captured values into it.
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
	s.URL, s.Body, s.Message, s.Target = replace(s.URL), replace(s.Body), replace(s.Message), replace(s.Target)
	return s
}

// captureFromHTTPBody walks dot-paths into the JSON body and stringifies
// the scalar leaf. A numeric segment indexes an array node ("devices.0.id"
// picks the first record's id of a wrapped list; "0.id" a top-level array) —
// out-of-range or non-array is the not-found error. A path prefixed with
// "length:" resolves to the array at that path and captures its element
// count (protocol-coupled UI assertions template list sizes into display
// text). Missing paths and non-scalar (or for length:, non-array) leaves are
// hard errors (clear failure over a silently-wrong later request — same
// policy as resolveURLParams).
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
		length := strings.HasPrefix(path, "length:")
		segs := strings.Split(strings.TrimPrefix(path, "length:"), ".")
		for _, seg := range segs {
			if m, isMap := cur.(map[string]any); isMap {
				leaf, ok = m[seg]
				if !ok {
					break
				}
			} else if arr, isArr := cur.([]any); isArr {
				idx, err := strconv.Atoi(seg)
				if err != nil || idx < 0 || idx >= len(arr) {
					ok = false
					break
				}
				leaf = arr[idx]
			} else {
				ok = false
				break
			}
			cur = leaf
		}
		if !ok {
			return nil, fmt.Errorf("capture: path %q not found in response", path)
		}
		if length {
			arr, isArr := leaf.([]any)
			if !isArr {
				return nil, fmt.Errorf("capture: path %q is not an array", path)
			}
			out[name] = fmt.Sprint(len(arr))
			continue
		}
		switch leaf.(type) {
		case map[string]any, []any:
			return nil, fmt.Errorf("capture: path %q is not a scalar", path)
		}
		out[name] = fmt.Sprint(leaf)
	}
	return out, nil
}

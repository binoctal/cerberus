package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsEnvironmentalFailure(t *testing.T) {
	tests := []struct {
		name string
		r    ExecutorResult
		want bool
	}{
		{"nil", nil, false},
		{"HTTP status 0 (connection refused)", HTTPResult{StatusCode: 0, URL: "http://x/y"}, true},
		{"HTTP status 200 (ok response)", HTTPResult{StatusCode: 200, URL: "http://x/y"}, false},
		{"HTTP status 500 (server error, reachable)", HTTPResult{StatusCode: 500, URL: "http://x/y"}, false},
		{"error result with connection refused", ErrorResult{Err: "connection refused"}, true},
		{"error result generic", ErrorResult{Err: "something else"}, false},
		{"browser error result generic", BrowserResult{OK: false, URL: "http://x"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsEnvironmentalFailure(tt.r))
		})
	}
}

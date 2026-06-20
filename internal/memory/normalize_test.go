package memory_test

import (
	"testing"

	"github.com/binoctal/cerberus/internal/memory"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeTarget(t *testing.T) {
	cases := map[string]string{
		"/api/users/123":     "/api/users/{id}",
		"/Users/123/":        "/users/{id}",
		"/orders/abcdef0123": "/orders/{id}",
		"/x?a=1":             "/x",
		"/GET /health":       "/get /health",
	}
	for in, want := range cases {
		assert.Equal(t, want, memory.NormalizeTarget(in), "input %q", in)
	}
	// Symmetry: a target and its templated form normalize the same.
	assert.Equal(t, memory.NormalizeTarget("/api/users/9"), memory.NormalizeTarget("/api/users/{id}"))
}

func TestNormalizeCondition(t *testing.T) {
	cases := map[string]string{
		"POST /api/v1/* returned 401": "post /api/v1/* returned 401",
		"  4xx   on  Login  ":         "4xx on login",
		"Auth failed.":                "auth failed",
	}
	for in, want := range cases {
		assert.Equal(t, want, memory.NormalizeCondition(in), "input %q", in)
	}
}

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestServiceHeadersMap(t *testing.T) {
	services := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Headers: map[string]string{"Host": "api.opendune.com"}},
		{Name: "router", URL: "http://localhost:8085", Headers: map[string]string{"X-Internal-Auth": "k"}},
		{Name: "nohdr", URL: "http://localhost:8086"}, // skipped: no headers
		{Name: "bad", URL: "://bad"}, // skipped: parse error
	}
	m := ServiceHeadersMap(services)
	assert.Equal(t, map[string]string{"Host": "api.opendune.com"}, m["localhost:8081"])
	assert.Equal(t, map[string]string{"X-Internal-Auth": "k"}, m["localhost:8085"])
	_, ok := m["localhost:8086"]
	assert.False(t, ok)
	assert.Len(t, m, 2)
}

func TestServiceHeadersMapEmpty(t *testing.T) {
	assert.Empty(t, ServiceHeadersMap(nil))
	assert.Empty(t, ServiceHeadersMap([]project.Service{{Name: "x", URL: "http://h:1"}}))
}

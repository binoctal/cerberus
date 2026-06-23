package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestToProjectServices_PortAndHealth(t *testing.T) {
	in := []NamedComposeService{
		{Name: "gateway", Service: ComposeService{
			Ports:       []string{"8081:8080"},
			Healthcheck: ComposeHealthcheck{Test: []string{"CMD", "wget", "--spider", "-q", "http://localhost:8080/health"}},
		}},
		{Name: "router", Service: ComposeService{
			Ports: []string{"8085"}, // short form → host 8085
		}},
	}
	got := ToProjectServices(in)
	assert.Equal(t, []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Health: "/health"},
		{Name: "router", URL: "http://localhost:8085", Health: ""},
	}, got)
}

func TestHostPort(t *testing.T) {
	cases := map[string]string{
		"8081:8080":           "8081",
		"127.0.0.1:8081:8080": "8081",
		"8085":                "8085",
		"":                    "",
	}
	for in, want := range cases {
		assert.Equal(t, want, hostPort(in), "input %q", in)
	}
}

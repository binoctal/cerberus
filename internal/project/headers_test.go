package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Service-level headers (shared across actors, e.g. Host for domain routing).
func TestServiceHeadersParse(t *testing.T) {
	input := `
services:
  - name: gateway
    url: "http://localhost:8081"
    headers:
      Host: api.opendune.com
      X-Internal-Auth: secret
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	require.Len(t, cfg.Services, 1)
	assert.Equal(t, map[string]string{
		"Host":            "api.opendune.com",
		"X-Internal-Auth": "secret",
	}, cfg.Services[0].Headers)
}

// Actor-level headers (per actor, e.g. Authorization: Bearer ...).
func TestCredentialHeadersParse(t *testing.T) {
	input := `
actors:
  - name: valid_user
    credentials:
      email: "x@y.z"
      headers:
        Authorization: "Bearer sk-relay-test"
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	require.Len(t, cfg.Actors, 1)
	assert.Equal(t, map[string]string{"Authorization": "Bearer sk-relay-test"},
		cfg.Actors[0].Credentials.Headers)
}

// Configs without headers parse fine (backward compatible).
func TestHeadersOptionalBackwardCompat(t *testing.T) {
	input := `
services:
  - name: web
    url: "http://localhost:3000"
actors:
  - name: anon
    credentials:
      email: "a@b.c"
`
	var cfg Config
	require.NoError(t, yaml.Unmarshal([]byte(input), &cfg))
	require.Len(t, cfg.Services, 1)
	assert.Nil(t, cfg.Services[0].Headers)
	assert.Nil(t, cfg.Actors[0].Credentials.Headers)
}

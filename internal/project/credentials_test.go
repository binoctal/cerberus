package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResolveActorCredentials_Found(t *testing.T) {
	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{Email: "admin@example.com", Password: "s3cret"}},
		},
	}

	email, pass, err := ResolveActorCredentials(cfg, "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin@example.com", email)
	assert.Equal(t, "s3cret", pass)
}

func TestResolveActorCredentials_NotFound(t *testing.T) {
	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{Email: "admin@example.com", Password: "s3cret"}},
		},
	}

	_, _, err := ResolveActorCredentials(cfg, "ghost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `actor "ghost" not found`)
}

// TestCredentialRef_TokenRoundTrip proves the static Token field is YAML-loadable
// (credentials.yaml, gitignored) and survives a marshal/unmarshal round-trip.
// omitempty is verified: an empty Token produces no token: line.
func TestCredentialRef_TokenRoundTrip(t *testing.T) {
	const yamlIn = `
email: admin@example.com
password: s3cret
token: demo_token
`
	var c CredentialRef
	require.NoError(t, yaml.Unmarshal([]byte(yamlIn), &c))
	require.Equal(t, "demo_token", c.Token, "token field must round-trip from YAML")

	out, err := yaml.Marshal(c)
	require.NoError(t, err)
	require.Contains(t, string(out), "token: demo_token", "token must marshal back to YAML")

	// omitempty: an empty Token must not emit a token: line.
	empty := CredentialRef{Email: "e", Password: "p"}
	outEmpty, err := yaml.Marshal(empty)
	require.NoError(t, err)
	require.NotContains(t, string(outEmpty), "token:", "empty Token must be omitted")
}

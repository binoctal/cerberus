package project

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

package protocoldiscover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgsToProtocol_FullShape(t *testing.T) {
	input := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
		"auth": map[string]any{
			"strategy":       "query",
			"param":          "token",
			"credential_ref": "web",
		},
		"roles": map[string]any{
			"web": map[string]any{
				"credential_ref": "web",
				"params":         map[string]any{"type": "web"},
				"handshake": map[string]any{
					"await_type": "devices:sync",
					"timeout":    5,
					"optional":   true,
				},
			},
		},
		"batches": map[string]any{
			"session:output-batch": map[string]any{
				"item_type":  "session:output",
				"items_path": "payload.lines",
			},
		},
	}

	p, err := argsToProtocol(input)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "json", p.Framing)
	assert.Equal(t, "type", p.TypePath)
	require.NotNil(t, p.Auth)
	assert.Equal(t, "web", p.Auth.CredentialRef)

	require.Contains(t, p.Roles, "web")
	web := p.Roles["web"]
	assert.Equal(t, "web", web.CredentialRef)
	require.NotNil(t, web.Handshake)
	assert.Equal(t, "devices:sync", web.Handshake.AwaitType)
	assert.Equal(t, 5, web.Handshake.Timeout)
	assert.True(t, web.Handshake.Optional, "optional must propagate")

	require.Contains(t, p.Batches, "session:output-batch")
	b := p.Batches["session:output-batch"]
	assert.Equal(t, "session:output", b.ItemType)
	assert.Equal(t, "payload.lines", b.ItemsPath)
}

func TestArgsToProtocol_OmitsAbsentOptionals(t *testing.T) {
	// Minimal input: no roles/batches/handshake. The assembled Protocol must
	// have nil maps, not zero-length placeholders, and no handshake.
	input := map[string]any{
		"found":     true,
		"framing":   "json",
		"type_path": "type",
	}
	p, err := argsToProtocol(input)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, p.Roles)
	assert.Nil(t, p.Batches)
	assert.Nil(t, p.Auth)
}

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

func TestProtocolDraftTool_SchemaCoversAllStructures(t *testing.T) {
	tool := protocolDraftTool()
	assert.Equal(t, "protocol_draft", tool.Name)

	top := tool.InputSchema
	assert.Equal(t, "object", top["type"])
	props := top["properties"].(map[string]any)

	for _, field := range []string{"found", "framing", "type_path", "auth", "roles", "batches", "notes"} {
		assert.Contains(t, props, field, "schema missing top-level field %q", field)
	}

	// roles.<role>.handshake must expose `optional` (peer-gated conditional
	// handshake). Navigate the nested object schema.
	rolesProp := props["roles"].(map[string]any)
	assert.Equal(t, "object", rolesProp["type"])
	rolesProps := rolesProp["additionalProperties"].(map[string]any)
	roleProps := rolesProps["properties"].(map[string]any)
	handshake := roleProps["handshake"].(map[string]any)
	handshakeProps := handshake["properties"].(map[string]any)
	assert.Contains(t, handshakeProps, "optional", "handshake schema must expose optional")

	// batches.<key> exposes item_type + items_path.
	batchesProp := props["batches"].(map[string]any)
	batchesProps := batchesProp["additionalProperties"].(map[string]any)
	batchProps := batchesProps["properties"].(map[string]any)
	assert.Contains(t, batchProps, "item_type")
	assert.Contains(t, batchProps, "items_path")
}

func TestProtocolDraftTool_HandshakeAndBatchExposeSource(t *testing.T) {
	tool := protocolDraftTool()
	top := tool.InputSchema
	props := top["properties"].(map[string]any)

	// roles.<role>.handshake.properties.source — the verbatim quote backing
	// await_type (must include the guard + type literal).
	rolesProp := props["roles"].(map[string]any)
	roleProps := rolesProp["additionalProperties"].(map[string]any)["properties"].(map[string]any)
	handshakeProps := roleProps["handshake"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, handshakeProps, "source", "handshake schema must expose a source quote field")

	// batches.<key>.properties.source — the verbatim flush-emit block.
	batchesProp := props["batches"].(map[string]any)
	batchProps := batchesProp["additionalProperties"].(map[string]any)["properties"].(map[string]any)
	assert.Contains(t, batchProps, "source", "batch schema must expose a source quote field")
}

func TestProtocolDraftTool_SchemaHasNoPathParam(t *testing.T) {
	tool := protocolDraftTool()
	props := tool.InputSchema["properties"].(map[string]any)
	for _, banned := range []string{"path_params", "url", "path"} {
		assert.NotContains(t, props, banned, "path-param concerns belong to the actor/auth layer, not Protocol")
	}
}

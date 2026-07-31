package protocoldiscover

import (
	"encoding/json"
	"fmt"

	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
)

// argsToProtocol assembles a *project.Protocol from a parsed protocol_draft
// tool-call input. It JSON-round-trips the map through inferOutput (the same
// struct shape the legacy Decide path used) so assembly stays in one place;
// the round-trip cannot leak credential values because Protocol has no field
// for them (credential_ref is an actor name, not a token).
func argsToProtocol(input map[string]any) (*project.Protocol, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal tool args: %w", err)
	}
	var out inferOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("parse tool args: %w", err)
	}

	p := &project.Protocol{
		Framing:  out.Framing,
		TypePath: out.TypePath,
	}
	if out.Auth != nil {
		p.Auth = &project.ProtocolAuth{
			Strategy:      out.Auth.Strategy,
			Param:         out.Auth.Param,
			CredentialRef: out.Auth.CredentialRef,
		}
	}
	if len(out.Roles) > 0 {
		p.Roles = map[string]*project.ProtocolRole{}
	}
	for name, r := range out.Roles {
		role := &project.ProtocolRole{
			CredentialRef: r.CredentialRef,
			Params:        r.Params,
			Headers:       r.Headers,
			Subprotocols:  r.Subprotocols,
		}
		if r.Handshake != nil {
			role.Handshake = &project.RoleHandshake{
				AwaitType: r.Handshake.AwaitType,
				Timeout:   r.Handshake.Timeout,
				Optional:  r.Handshake.Optional,
			}
		}
		p.Roles[name] = role
	}
	if len(out.Batches) > 0 {
		p.Batches = map[string]*project.ProtocolBatch{}
	}
	for key, b := range out.Batches {
		p.Batches[key] = &project.ProtocolBatch{
			ItemType:  b.ItemType,
			ItemsPath: b.ItemsPath,
		}
	}
	return p, nil
}

// protocolDraftTool is the typed tool Infer offers the LLM. Its InputSchema is
// hand-written (cerberus has no struct->schema reflection; this mirrors Scout's
// tools.go). The schema is the inferable subset of project.Protocol plus a
// `found` flag that lets the model explicitly signal "no WS protocol here"
// (distinct from drift — see Infer's zero-tool-call handling).
func protocolDraftTool() llm.Tool {
	str := func() map[string]any { return map[string]any{"type": "string"} }
	return llm.Tool{
		Name:        "protocol_draft",
		Description: "Draft a WebSocket protocol declaration from the provided docs/source. Call this once; set found=false if no WS protocol is described.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"found":     map[string]any{"type": "boolean", "description": "true if a WS protocol is described; false otherwise."},
				"framing":   map[string]any{"type": "string", "enum": []any{"json", "text", "binary"}},
				"type_path": map[string]any{"type": "string", "description": "Dotted JSON path to the routing key (e.g. \"type\", \"payload.kind\")."},
				"auth": map[string]any{"type": "object", "properties": map[string]any{
					"strategy":       map[string]any{"type": "string", "enum": []any{"query", "header", "subprotocol"}},
					"param":          str(),
					"credential_ref": str(),
				}},
				"roles": map[string]any{
					"type":        "object",
					"description": "Named connection types (e.g. web, bridge). Keys are role names.",
					"additionalProperties": map[string]any{"type": "object", "properties": map[string]any{
						"credential_ref": str(),
						"params":         map[string]any{"type": "object"},
						"headers":        map[string]any{"type": "object"},
						"subprotocols":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"handshake": map[string]any{"type": "object", "description": "Mandatory/best-effort post-connect exchange.", "properties": map[string]any{
							"await_type": str(),
							"timeout":    map[string]any{"type": "number"},
							"optional":   map[string]any{"type": "boolean", "description": "true = best-effort: a timeout still succeeds the connect (peer-gated handshake)."},
						}},
					}},
				},
				"batches": map[string]any{
					"type":        "object",
					"description": "Batch decomposition: when a frame's routing key matches a key here, expand the array at items_path into item_type frames.",
					"additionalProperties": map[string]any{"type": "object", "properties": map[string]any{
						"item_type":  str(),
						"items_path": str(),
					}},
				},
				"notes": str(),
			},
			"required": []any{"found"},
		},
	}
}

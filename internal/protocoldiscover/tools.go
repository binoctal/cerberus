package protocoldiscover

import (
	"encoding/json"
	"fmt"

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

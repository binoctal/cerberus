package project

import "fmt"

// validProtocolFraming is the set of framing values the WS executor supports.
var validProtocolFraming = map[string]bool{"": true, "json": true, "text": true, "binary": true}

// validProtocolAuthStrategy is the set of auth placement strategies.
var validProtocolAuthStrategy = map[string]bool{"query": true, "header": true, "subprotocol": true}

// ValidateProtocol checks a Protocol declaration for config-time errors. A nil
// protocol is valid (means M0 fallback). actors is the config's actor list, used
// to confirm credential_ref names a real actor. Returns nil if valid.
func ValidateProtocol(p *Protocol, actors []Actor) error {
	if p == nil {
		return nil
	}
	if !validProtocolFraming[p.Framing] {
		return fmt.Errorf("protocol.framing %q must be json, text, or binary", p.Framing)
	}
	if p.Auth != nil {
		if !validProtocolAuthStrategy[p.Auth.Strategy] {
			return fmt.Errorf("protocol.auth.strategy %q must be query, header, or subprotocol", p.Auth.Strategy)
		}
		if p.Auth.Param == "" {
			return fmt.Errorf("protocol.auth.param is required when strategy is set")
		}
		if p.Auth.CredentialRef != "" {
			found := false
			for _, a := range actors {
				if a.Name == p.Auth.CredentialRef {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("protocol.auth.credential_ref %q does not match any actor", p.Auth.CredentialRef)
			}
		}
	}
	for name, role := range p.Roles {
		if role.CredentialRef != "" {
			found := false
			for _, a := range actors {
				if a.Name == role.CredentialRef {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("roles[%q].credential_ref %q does not match any actor", name, role.CredentialRef)
			}
		}
		if p.Auth != nil {
			// A role must not occupy the auth token slot on the carrier auth
			// uses; on other carriers the same name is harmless (different slot).
			switch p.Auth.Strategy {
			case "query":
				for k := range role.Params {
					if k == p.Auth.Param {
						return fmt.Errorf("roles[%q].params[%q] collides with auth.param (token slot)", name, k)
					}
				}
			case "header":
				for k := range role.Headers {
					if k == p.Auth.Param {
						return fmt.Errorf("roles[%q].headers[%q] collides with auth.param (token slot)", name, k)
					}
				}
			case "subprotocol":
				for _, s := range role.Subprotocols {
					if s == p.Auth.Param {
						return fmt.Errorf("roles[%q].subprotocols[%q] collides with auth.param (token slot)", name, s)
					}
				}
			}
		}
		if role.Handshake != nil {
			if role.Handshake.AwaitType == "" {
				return fmt.Errorf("roles[%q].handshake.await_type is required", name)
			}
			if role.Handshake.Timeout <= 0 {
				return fmt.Errorf("roles[%q].handshake.timeout must be > 0", name)
			}
		}
	}
	// Batch decomposition iterates a JSON array, so it is defined only for json
	// framing ("" defaults to json). Reject a batch declaration under text/binary.
	if len(p.Batches) > 0 && p.Framing != "" && p.Framing != "json" {
		return fmt.Errorf("batches require json framing, got %q", p.Framing)
	}
	for bName, batch := range p.Batches {
		if batch == nil || batch.ItemType == "" {
			return fmt.Errorf("batches[%q].item_type is required", bName)
		}
		if batch.ItemsPath == "" {
			return fmt.Errorf("batches[%q].items_path is required", bName)
		}
		// A batch whose item type is itself a batch would recurse in the pump;
		// reject so decomposition stays single-level.
		if _, recurse := p.Batches[batch.ItemType]; recurse {
			return fmt.Errorf("batches[%q].item_type %q is itself a batch (recursive decomposition is not allowed)", bName, batch.ItemType)
		}
	}
	return nil
}

// validateProtocol is Phase 6 of Config.Validate: validates each service's
// optional Protocol block, collecting all errors.
func validateProtocol(cfg *Config, ve *ValidationError) {
	for i, svc := range cfg.Services {
		if svc.Protocol == nil {
			continue
		}
		if err := ValidateProtocol(svc.Protocol, cfg.Actors); err != nil {
			ve.add(fmt.Sprintf("services[%d].%s", i, err.Error()))
		}
	}
}

package project

import "fmt"

// validProtocolFraming is the set of framing values the WS executor supports.
var validProtocolFraming = map[string]bool{"": true, "json": true, "text": true, "binary": true}

// validProtocolAuthStrategy is the set of auth placement strategies.
var validProtocolAuthStrategy = map[string]bool{"query": true, "header": true, "subprotocol": true}

// actorExists reports whether name matches a declared actor. Shared by the auth
// and role credential_ref checks.
func actorExists(actors []Actor, name string) bool {
	for _, a := range actors {
		if a.Name == name {
			return true
		}
	}
	return false
}

// paramCollision returns a non-nil error if role occupies the same carrier slot
// that auth.param uses. The slot label in the message (params/headers/
// subprotocols) differs by strategy, so the message is constructed here to keep
// the literal in one place.
func paramCollision(name string, role *ProtocolRole, auth *ProtocolAuth) error {
	switch auth.Strategy {
	case "query":
		for k := range role.Params {
			if k == auth.Param {
				return fmt.Errorf("roles[%q].params[%q] collides with auth.param (token slot)", name, k)
			}
		}
	case "header":
		for k := range role.Headers {
			if k == auth.Param {
				return fmt.Errorf("roles[%q].headers[%q] collides with auth.param (token slot)", name, k)
			}
		}
	case "subprotocol":
		for _, s := range role.Subprotocols {
			if s == auth.Param {
				return fmt.Errorf("roles[%q].subprotocols[%q] collides with auth.param (token slot)", name, s)
			}
		}
	}
	return nil
}

// validateProtocolAuth checks the auth block in isolation.
func validateProtocolAuth(auth *ProtocolAuth, actors []Actor) error {
	if !validProtocolAuthStrategy[auth.Strategy] {
		return fmt.Errorf("protocol.auth.strategy %q must be query, header, or subprotocol", auth.Strategy)
	}
	if auth.Param == "" {
		return fmt.Errorf("protocol.auth.param is required when strategy is set")
	}
	if auth.CredentialRef != "" && !actorExists(actors, auth.CredentialRef) {
		return fmt.Errorf("protocol.auth.credential_ref %q does not match any actor", auth.CredentialRef)
	}
	return nil
}

// validateRole checks one named role's credential_ref, auth-slot collision, and
// handshake.
func validateRole(name string, role *ProtocolRole, auth *ProtocolAuth, actors []Actor) error {
	if role.CredentialRef != "" && !actorExists(actors, role.CredentialRef) {
		return fmt.Errorf("roles[%q].credential_ref %q does not match any actor", name, role.CredentialRef)
	}
	if auth != nil {
		if err := paramCollision(name, role, auth); err != nil {
			return err
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
	return nil
}

// validateProtocolBatches checks the framing precondition and each batch entry.
func validateProtocolBatches(batches map[string]*ProtocolBatch, framing string) error {
	// Batch decomposition iterates a JSON array, so it is defined only for json
	// framing ("" defaults to json). Reject a batch declaration under text/binary.
	if len(batches) > 0 && framing != "" && framing != "json" {
		return fmt.Errorf("batches require json framing, got %q", framing)
	}
	for bName, batch := range batches {
		if batch == nil || batch.ItemType == "" {
			return fmt.Errorf("batches[%q].item_type is required", bName)
		}
		if batch.ItemsPath == "" {
			return fmt.Errorf("batches[%q].items_path is required", bName)
		}
		// A batch whose item type is itself a batch would recurse in the pump;
		// reject so decomposition stays single-level.
		if _, recurse := batches[batch.ItemType]; recurse {
			return fmt.Errorf("batches[%q].item_type %q is itself a batch (recursive decomposition is not allowed)", bName, batch.ItemType)
		}
	}
	return nil
}

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
		if err := validateProtocolAuth(p.Auth, actors); err != nil {
			return err
		}
	}
	for name, role := range p.Roles {
		if err := validateRole(name, role, p.Auth, actors); err != nil {
			return err
		}
	}
	return validateProtocolBatches(p.Batches, p.Framing)
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

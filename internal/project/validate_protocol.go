package project

import "fmt"

// validProtocolFraming is the set of framing values M1 supports. M2 may add
// "text"/"binary".
var validProtocolFraming = map[string]bool{"": true, "json": true}

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
		return fmt.Errorf("protocol.framing %q is not supported in M1 (use \"json\")", p.Framing)
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

package project

import (
	"fmt"
)

// validateActors checks actor configuration
func validateActors(cfg *Config, ve *ValidationError) {
	seenActor := make(map[string]bool)
	for i, a := range cfg.Actors {
		if a.Name == "" {
			ve.add(fmt.Sprintf("actors[%d]: name is required", i))
		} else if seenActor[a.Name] {
			ve.add(fmt.Sprintf("actors[%d]: duplicate actor name %q", i, a.Name))
		} else {
			seenActor[a.Name] = true
		}
		if msg := validateAuthFlow(i, a); msg != "" {
			ve.add(msg)
		}
		if msg := validateGeneratedPathParams(i, a); msg != "" {
			ve.add(msg)
		}
		if msg := validateFidelity(i, a); msg != "" {
			ve.add(msg)
		}
	}
}

// validateFidelity checks the per-actor fidelity manifest: real-process actors
// require a process block with a start command, emulated actors must not carry
// one, and the value must be a known fidelity.
func validateFidelity(actorIdx int, a Actor) string {
	switch a.Fidelity {
	case "", FidelityEmulated:
		if a.Process != nil {
			return fmt.Sprintf("actors[%d]: fidelity %q must not have a process block", actorIdx, FidelityEmulated)
		}
	case FidelityRealProcess:
		if a.Process == nil || len(a.Process.Start) == 0 {
			return fmt.Sprintf("actors[%d]: fidelity %q: process block is required (with a start command)", actorIdx, FidelityRealProcess)
		}
	default:
		return fmt.Sprintf("actors[%d]: unknown fidelity %q (supported: %s, %s)", actorIdx, a.Fidelity, FidelityEmulated, FidelityRealProcess)
	}
	return ""
}

// validateGeneratedPathParams checks declared generated_path_params: keys are
// identifier-like (a single {placeholder} name, same rule as auth.path_params),
// values name a supported generator, and a name must not ALSO be declared as a
// captured auth.path_param (that would be ambiguous — generated would silently
// overwrite captured at merge). Runs for every actor regardless of auth.
func validateGeneratedPathParams(actorIdx int, a Actor) string {
	for name, kind := range a.GeneratedPathParams {
		if !pathParamNameRE.MatchString(name) {
			return fmt.Sprintf("actors[%d].generated_path_params: key %q is not a valid param name (must match %s)", actorIdx, name, pathParamNameRE.String())
		}
		if !supportedPathParamGenerators[kind] {
			return fmt.Sprintf("actors[%d].generated_path_params: %q has unknown generator %q (supported: uuid)", actorIdx, name, kind)
		}
		if a.Auth != nil {
			if _, dup := a.Auth.PathParams[name]; dup {
				return fmt.Sprintf("actors[%d].generated_path_params: %q is also declared in auth.path_params; a param must be either captured or generated, not both", actorIdx, name)
			}
		}
	}
	return ""
}

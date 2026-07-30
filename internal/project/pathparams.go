package project

import (
	"fmt"

	"github.com/google/uuid"
)

// supportedPathParamGenerators is the vocabulary of runtime path-param
// generators. A generated_path_params entry's value names one of these; the
// resolved value is synthesized at session setup (NOT captured from an auth
// response). Add a case to ResolveGeneratedPathParams to extend.
var supportedPathParamGenerators = map[string]bool{"uuid": true}

// ResolveGeneratedPathParams synthesizes a concrete value for each declared
// generator (name -> kind). Returns an error naming the first unknown generator
// kind. Pure; safe to call directly from tests. Used by session setup to merge
// generated values into Credentials.PathParams so the existing WS templating
// pipeline substitutes {name} placeholders at connect.
func ResolveGeneratedPathParams(spec map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(spec))
	for name, kind := range spec {
		switch kind {
		case "uuid":
			out[name] = uuid.NewString()
		default:
			return nil, fmt.Errorf("generated_path_params: %q has unknown generator %q (supported: uuid)", name, kind)
		}
	}
	return out, nil
}

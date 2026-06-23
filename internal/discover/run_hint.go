package discover

import "github.com/binoctal/cerberus/internal/project"

// HintMessage is printed by `cerberus run` when services look unconfigured.
const HintMessage = "hint: docker-compose.yml present but no services configured. Run `cerberus discover` to generate them, then fill domain/path_prefix/key."

// ShouldHintDiscover reports whether run should nudge the user toward discover.
func ShouldHintDiscover(services []project.Service, composeExists bool) bool {
	return len(services) == 0 && composeExists
}

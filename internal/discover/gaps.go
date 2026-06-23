package discover

import (
	"fmt"
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// ServiceGaps records which required-by-test fields a discovered service lacks.
type ServiceGaps struct {
	Name             string
	MissingHost      bool // domain routing (Host header)
	MissingPathPrefix bool // for block ① service attribution
}

// Gaps inspects services and returns one entry per service missing a field.
func Gaps(services []project.Service) []ServiceGaps {
	var out []ServiceGaps
	for _, s := range services {
		g := ServiceGaps{Name: s.Name}
		if s.Headers["Host"] == "" {
			g.MissingHost = true
		}
		if len(s.PathPrefix) == 0 {
			g.MissingPathPrefix = true
		}
		if g.MissingHost || g.MissingPathPrefix {
			out = append(out, g)
		}
	}
	return out
}

// FormatGaps renders a human-readable gap report. hasActorKey indicates
// whether at least one actor carries credentials (auth key); when false the
// report reminds the user to add an actor key.
func FormatGaps(gaps []ServiceGaps, hasActorKey bool) string {
	var b strings.Builder
	if len(gaps) > 0 {
		b.WriteString("gaps (fill manually in project.yaml):\n")
		for _, g := range gaps {
			var missing []string
			if g.MissingHost {
				missing = append(missing, "domain (Host header)")
			}
			if g.MissingPathPrefix {
				missing = append(missing, "path_prefix")
			}
			fmt.Fprintf(&b, "  %s: needs %s\n", g.Name, strings.Join(missing, ", "))
		}
	}
	if !hasActorKey {
		b.WriteString("gaps: add at least one actor with a credentials key (Bearer) for authenticated paths\n")
	}
	return b.String()
}

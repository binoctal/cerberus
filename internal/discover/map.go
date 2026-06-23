package discover

import (
	"strings"

	"github.com/binoctal/cerberus/internal/project"
)

// hostPort extracts the host-side port from a docker-compose ports entry.
// "8081:8080" → "8081"; "127.0.0.1:8081:8080" → "8081"; "8085" → "8085".
func hostPort(entry string) string {
	if entry == "" {
		return ""
	}
	parts := strings.Split(entry, ":")
	switch len(parts) {
	case 1:
		return parts[0] // "8085"
	case 2:
		return parts[0] // "8081:8080"
	default:
		return parts[len(parts)-2] // "127.0.0.1:8081:8080"
	}
}

// healthPath extracts the path from a healthcheck test list, finding the
// first http(s) URL and returning its path; "" if none.
func healthPath(test []string) string {
	for _, tok := range test {
		for _, scheme := range []string{"http://", "https://"} {
			if i := strings.Index(tok, scheme); i >= 0 {
				u := tok[i+len(scheme):]
				// strip host[:port]
				if slash := strings.Index(u, "/"); slash >= 0 {
					return u[slash:]
				}
			}
		}
	}
	return ""
}

// ToProjectServices maps filtered compose services to cerberus project.Service
// values with localhost URLs and (best-effort) health paths.
func ToProjectServices(in []NamedComposeService) []project.Service {
	out := make([]project.Service, 0, len(in))
	for _, n := range in {
		port := ""
		if len(n.Service.Ports) > 0 {
			port = hostPort(n.Service.Ports[0])
		}
		url := ""
		if port != "" {
			url = "http://localhost:" + port
		}
		out = append(out, project.Service{
			Name:   n.Name,
			URL:    url,
			Health: healthPath(n.Service.Healthcheck.Test),
		})
	}
	return out
}

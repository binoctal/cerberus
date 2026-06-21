package agent

import (
	"net/url"

	"github.com/binoctal/cerberus/internal/project"
)

// ServiceHeadersMap builds a "host:port" → headers index from services, for
// the HTTP executor's service-level header injection. Services without headers
// or with an unparseable URL are skipped.
func ServiceHeadersMap(services []project.Service) map[string]map[string]string {
	out := make(map[string]map[string]string)
	for _, svc := range services {
		if len(svc.Headers) == 0 {
			continue
		}
		u, err := url.Parse(svc.URL)
		if err != nil {
			continue
		}
		out[u.Host] = svc.Headers
	}
	return out
}

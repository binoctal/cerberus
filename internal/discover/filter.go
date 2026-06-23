package discover

import "strings"

// NamedComposeService pairs a service name with its definition.
type NamedComposeService struct {
	Name    string
	Service ComposeService
}

// infraImageSubstrings identifies infra images by substring. Conservative:
// a few well-known names; users override with --include for anything missed.
var infraImageSubstrings = []string{
	"postgres", "pgvector", "mysql", "mariadb", "redis", "memcached",
	"mongo", "kafka", "zookeeper", "rabbitmq", "nginx", "traefik",
	"minio", "elastic", "consul", "etcd",
}

func isInfraImage(image string) bool {
	low := strings.ToLower(image)
	for _, s := range infraImageSubstrings {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// FilterServices drops infra images and portless services, then applies
// explicit --include (force keep) and --exclude (force drop) overrides.
func FilterServices(services map[string]ComposeService, include, exclude []string) []NamedComposeService {
	var out []NamedComposeService
	for name, svc := range services {
		if contains(exclude, name) {
			continue
		}
		if contains(include, name) {
			out = append(out, NamedComposeService{Name: name, Service: svc})
			continue
		}
		if len(svc.Ports) == 0 {
			continue
		}
		if isInfraImage(svc.Image) {
			continue
		}
		out = append(out, NamedComposeService{Name: name, Service: svc})
	}
	return out
}

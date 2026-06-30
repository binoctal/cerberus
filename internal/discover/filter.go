package discover

import (
	"fmt"
	"sort"
	"strings"
)

// NamedComposeService pairs a service name with its definition.
type NamedComposeService struct {
	Name    string
	Service ComposeService
}

// DropReason describes why a service was filtered out.
type DropReason struct {
	Name   string
	Reason string
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
// Returns the kept services and a list of dropped services with reasons.
func FilterServices(services map[string]ComposeService, include, exclude []string) ([]NamedComposeService, []DropReason) {
	var out []NamedComposeService
	var dropped []DropReason
	for name, svc := range services {
		if contains(exclude, name) {
			dropped = append(dropped, DropReason{Name: name, Reason: "excluded via --exclude"})
			continue
		}
		if contains(include, name) {
			out = append(out, NamedComposeService{Name: name, Service: svc})
			continue
		}
		if len(svc.Ports) == 0 {
			dropped = append(dropped, DropReason{Name: name, Reason: "no ports exposed"})
			continue
		}
		if isInfraImage(svc.Image) {
			dropped = append(dropped, DropReason{Name: name, Reason: "infrastructure image"})
			continue
		}
		out = append(out, NamedComposeService{Name: name, Service: svc})
	}
	// Sort by name for deterministic output
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	sort.Slice(dropped, func(i, j int) bool {
		return dropped[i].Name < dropped[j].Name
	})
	return out, dropped
}

// FormatDroppedServices formats a list of dropped service reasons for display.
func FormatDroppedServices(dropped []DropReason) string {
	if len(dropped) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("Filtered services:\n")
	for _, d := range dropped {
		fmt.Fprintf(&sb, "  - %s (%s)\n", d.Name, d.Reason)
	}
	return sb.String()
}

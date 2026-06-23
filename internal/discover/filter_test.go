package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterServices_DropsInfraAndPortless(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":   {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres":  {Image: "postgres:16", Ports: []string{"5432:5432"}},
		"redis":     {Image: "redis:7", Ports: []string{"6379:6379"}},
		"edgeless":  {Image: "relay-edge:dev"}, // no ports → dropped
	}
	got := FilterServices(services, nil, nil)
	names := namesOf(got)
	assert.Contains(t, names, "gateway")
	assert.NotContains(t, names, "postgres")
	assert.NotContains(t, names, "redis")
	assert.NotContains(t, names, "edgeless")
}

func TestFilterServices_IncludeExcludeOverride(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":  {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres": {Image: "postgres:16", Ports: []string{"5432:5432"}},
	}
	// include forces postgres back in; exclude drops gateway
	got := FilterServices(services, []string{"postgres"}, []string{"gateway"})
	names := namesOf(got)
	assert.Contains(t, names, "postgres")
	assert.NotContains(t, names, "gateway")
}

func namesOf(s []NamedComposeService) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = v.Name
	}
	return out
}

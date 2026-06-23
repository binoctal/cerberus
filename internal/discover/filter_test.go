package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterServices_DropsInfraAndPortless(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":  {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres": {Image: "postgres:16", Ports: []string{"5432:5432"}},
		"redis":    {Image: "redis:7", Ports: []string{"6379:6379"}},
		"edgeless": {Image: "relay-edge:dev"}, // no ports → dropped
	}
	got, dropped := FilterServices(services, nil, nil)
	names := namesOf(got)
	assert.Contains(t, names, "gateway")
	assert.NotContains(t, names, "postgres")
	assert.NotContains(t, names, "redis")
	assert.NotContains(t, names, "edgeless")
	// Check dropped reasons
	droppedNames := make(map[string]string)
	for _, d := range dropped {
		droppedNames[d.Name] = d.Reason
	}
	assert.Equal(t, "infrastructure image", droppedNames["postgres"])
	assert.Equal(t, "infrastructure image", droppedNames["redis"])
	assert.Equal(t, "no ports exposed", droppedNames["edgeless"])
}

func TestFilterServices_IncludeExcludeOverride(t *testing.T) {
	services := map[string]ComposeService{
		"gateway":  {Image: "relay-gateway:dev", Ports: []string{"8081:8080"}},
		"postgres": {Image: "postgres:16", Ports: []string{"5432:5432"}},
	}
	// include forces postgres back in; exclude drops gateway
	got, dropped := FilterServices(services, []string{"postgres"}, []string{"gateway"})
	names := namesOf(got)
	assert.Contains(t, names, "postgres")
	assert.NotContains(t, names, "gateway")
	// Check dropped reason
	assert.Len(t, dropped, 1)
	assert.Equal(t, "gateway", dropped[0].Name)
	assert.Equal(t, "excluded via --exclude", dropped[0].Reason)
}

func namesOf(s []NamedComposeService) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = v.Name
	}
	return out
}

func TestFormatDroppedServices(t *testing.T) {
	dropped := []DropReason{
		{Name: "postgres", Reason: "infrastructure image"},
		{Name: "redis", Reason: "infrastructure image"},
	}
	got := FormatDroppedServices(dropped)
	assert.Contains(t, got, "Filtered services:")
	assert.Contains(t, got, "- postgres (infrastructure image)")
	assert.Contains(t, got, "- redis (infrastructure image)")

	// Empty list returns empty string
	assert.Equal(t, "", FormatDroppedServices(nil))
	assert.Equal(t, "", FormatDroppedServices([]DropReason{}))
}

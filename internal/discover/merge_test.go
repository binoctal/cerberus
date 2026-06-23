package discover

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/binoctal/cerberus/internal/project"
)

func TestMergeIntoConfig_PreservesExistingAppendsNew(t *testing.T) {
	cfg := &project.Config{}
	cfg.Services = []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Headers: map[string]string{"Host": "api.modelsite.ai"}}, // hand-written override
	}
	discovered := []project.Service{
		{Name: "gateway", URL: "http://localhost:8081", Health: "/health"}, // must NOT overwrite
		{Name: "admin", URL: "http://localhost:8086", Health: "/health"},   // appended
	}
	added := MergeIntoConfig(cfg, discovered)
	assert.Equal(t, []string{"admin"}, added)
	assert.Len(t, cfg.Services, 2)
	// gateway kept its Host override, Health not filled (override preserved)
	assert.Equal(t, "api.modelsite.ai", cfg.Services[0].Headers["Host"])
	assert.Equal(t, "", cfg.Services[0].Health)
	assert.Equal(t, "/health", cfg.Services[1].Health)
}

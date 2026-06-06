package project

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestProjectYAMLParse(t *testing.T) {
	input := `
project:
  name: my-saas
services:
  - name: web
    url: "http://localhost:3000"
    health: "/"
  - name: api
    url: "http://localhost:8080"
actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
    entry: "/admin"
databases:
  - name: main
    url: "${DATABASE_URL}"
invariants:
  - id: INV-001
    description: "balance cannot be negative"
    severity: critical
    check: "SELECT COUNT(*) AS cnt FROM users WHERE balance < 0"
    assertion: "cnt == 0"
settings:
  max_duration: 30m
  confidence_threshold: 0.7
  auto_fix: low_only
`
	var cfg Config
	err := yaml.Unmarshal([]byte(input), &cfg)
	require.NoError(t, err)

	assert.Equal(t, "my-saas", cfg.Project.Name)
	require.Len(t, cfg.Services, 2)
	assert.Equal(t, "http://localhost:3000", cfg.Services[0].URL)
	require.Len(t, cfg.Actors, 1)
	assert.Equal(t, "${ADMIN_EMAIL}", cfg.Actors[0].Credentials.Email)
	require.Len(t, cfg.Databases, 1)
	require.Len(t, cfg.Invariants, 1)
	assert.Equal(t, "cnt == 0", cfg.Invariants[0].Assertion)
	assert.Equal(t, 0.7, cfg.Settings.ConfidenceThreshold)
}

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 200000, cfg.Settings.AIBudget.SessionTotalTokens)
	assert.Equal(t, 10000, cfg.Settings.AIBudget.PerCallLimit)
	assert.Equal(t, 0.7, cfg.Settings.ConfidenceThreshold)
}

func TestLoaderEnvInterpolation(t *testing.T) {
	os.Setenv("ADMIN_EMAIL", "admin@test.dev")
	os.Setenv("ADMIN_PASS", "secret123")
	os.Setenv("DATABASE_URL", "postgres://localhost/mydb")
	defer func() {
		os.Unsetenv("ADMIN_EMAIL")
		os.Unsetenv("ADMIN_PASS")
		os.Unsetenv("DATABASE_URL")
	}()

	input := `
project:
  name: test
actors:
  - name: admin
    credentials:
      email: "${ADMIN_EMAIL}"
      password: "${ADMIN_PASS}"
databases:
  - name: main
    url: "${DATABASE_URL}"
`
	cfg, err := LoadFromYAML([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "admin@test.dev", cfg.Actors[0].Credentials.Email)
	assert.Equal(t, "secret123", cfg.Actors[0].Credentials.Password)
	assert.Equal(t, "postgres://localhost/mydb", cfg.Databases[0].URL)
}

func TestCredentialResolution(t *testing.T) {
	os.Setenv("CERBERUS_ACTOR_ADMIN_EMAIL", "env-admin@test.dev")
	os.Setenv("CERBERUS_ACTOR_ADMIN_PASSWORD", "env-secret")
	defer func() {
		os.Unsetenv("CERBERUS_ACTOR_ADMIN_EMAIL")
		os.Unsetenv("CERBERUS_ACTOR_ADMIN_PASSWORD")
	}()

	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{
				Email: "file-admin@test.dev", Password: "file-secret",
			}},
		},
	}
	resolved := ResolveCredentials(cfg)
	assert.Equal(t, "env-admin@test.dev", resolved.Actors[0].Credentials.Email)
	assert.Equal(t, "env-secret", resolved.Actors[0].Credentials.Password)
}

func TestCredentialFileFallback(t *testing.T) {
	cfg := &Config{
		Actors: []Actor{
			{Name: "admin", Credentials: CredentialRef{
				Email: "file-admin@test.dev", Password: "file-secret",
			}},
		},
	}
	resolved := ResolveCredentials(cfg)
	assert.Equal(t, "file-admin@test.dev", resolved.Actors[0].Credentials.Email)
}

func TestProjectModelInfoScore(t *testing.T) {
	// Empty model → score 0
	pm := &ProjectModel{}
	assert.InDelta(t, 0.0, pm.InfoScore(false), 0.01)

	// Model with some endpoints + pages, no schema, no history
	pm = &ProjectModel{
		Navigation: NavigationModel{Pages: []PageDef{
			{Path: "/", Confidence: 0.9},
			{Path: "/login", Confidence: 0.9},
			{Path: "/admin", Confidence: 0.8},
		}},
		API: APIModel{Endpoints: []EndpointDef{
			{Method: "GET", Path: "/api/v1/users", Confidence: 0.95},
			{Method: "POST", Path: "/api/v1/users", Confidence: 0.95},
		}},
		SchemaAnalyzed: false,
	}
	score := pm.InfoScore(false)
	// endpointScore = min(2,20)/20 * 0.95 * 0.4 = 0.1*0.95*0.4 = 0.038
	// pageScore = min(3,30)/30 * avg(0.9,0.9,0.8) * 0.3 = 0.1*0.867*0.3 = 0.026
	// total ≈ 0.064
	assert.Greater(t, score, 0.05)
	assert.Less(t, score, 0.1)

	// With schema + history → significant boost
	pm.SchemaAnalyzed = true
	scoreWithHistory := pm.InfoScore(true)
	assert.Greater(t, scoreWithHistory, score, "history should increase score")

	// Saturated model (20+ endpoints, 30+ pages, all high confidence)
	saturated := &ProjectModel{
		Navigation: NavigationModel{Pages: makePages(35, 0.95)},
		API:        APIModel{Endpoints: makeEndpoints(25, 0.95)},
	}
	saturated.SchemaAnalyzed = true
	satScore := saturated.InfoScore(true)
	assert.Greater(t, satScore, 0.8, "saturated model should score high")
	assert.LessOrEqual(t, satScore, 1.0, "score should not exceed 1.0")
}

func makePages(n int, conf float64) []PageDef {
	pages := make([]PageDef, n)
	for i := range pages {
		pages[i] = PageDef{Path: fmt.Sprintf("/page/%d", i), Confidence: conf}
	}
	return pages
}

func makeEndpoints(n int, conf float64) []EndpointDef {
	endpoints := make([]EndpointDef, n)
	for i := range endpoints {
		endpoints[i] = EndpointDef{Method: "GET", Path: fmt.Sprintf("/api/v1/res%d", i), Confidence: conf}
	}
	return endpoints
}

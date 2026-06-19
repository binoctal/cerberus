package scout

import (
	"encoding/json"
	"fmt"
	"strings"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	embedPkg "github.com/binoctal/cerberus/internal/embed"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// Scout performs project reconnaissance: analyze to build a cognitive model,
// then plan to generate a test plan.
type Scout struct {
	driver         *ai.Driver
	store          *store.Store
	config         *project.Config
	logger         *zap.Logger
	deepPlan       bool                      // Enable ToT deep planning mode
	totCfg         ToTConfig                 // ToT configuration (only used when deepPlan=true)
	proposeDriver  *ai.Driver                // ToT propose driver (SONNET tier); nil → driver
	evaluateDriver *ai.Driver                // ToT evaluate driver (HAIKU tier); nil → driver
	reflexionCfg   project.ReflexionSettings // cross-session memory recall knobs (defaults 10/5/0.3)
	embedder       embedPkg.Provider         // embedding provider for semantic search
}

// NewScout creates a Scout head.
func NewScout(driver *ai.Driver, store *store.Store, config *project.Config, logger *zap.Logger) *Scout {
	return &Scout{
		driver:       driver,
		store:        store,
		config:       config,
		logger:       logger,
		reflexionCfg: project.ReflexionSettings{EpisodicLimit: 10, SemanticTopK: 5, SemanticThreshold: 0.3},
		embedder:     embedPkg.NewTrigramProvider(embedPkg.DefaultDimension),
	}
}

// SetDeepPlan enables ToT deep planning mode with the given config and the two
// tiered drivers ToT uses: proposeDriver for strategy generation (SONNET tier),
// evaluateDriver for scoring (HAIKU tier). Either may be nil to fall back to
// the Scout's shared driver (e.g. when running standalone without tier
// detection).
func (s *Scout) SetDeepPlan(cfg ToTConfig, proposeDriver, evaluateDriver *ai.Driver) {
	s.deepPlan = true
	s.totCfg = cfg
	s.proposeDriver = proposeDriver
	s.evaluateDriver = evaluateDriver
}

// SetReflexion configures cross-session memory recall knobs used by
// buildEpisodicContext (episodic_limit, semantic_topk, semantic_threshold).
// Callers pass config.ResolveReflexionConfig, which fills defaults for unset
// fields; omitting the call keeps the built-in defaults (10/5/0.3).
func (s *Scout) SetReflexion(rs project.ReflexionSettings) {
	s.reflexionCfg = rs
}

// isLocalOnly reports whether there is no live HTTP target. Explicit Mode
// (local|saas) takes precedence; otherwise it infers from services (no
// service URL → local codebase), which is backward compatible.
func (s *Scout) isLocalOnly() bool {
	switch s.config.Settings.Mode {
	case project.ModeLocal:
		return true
	case project.ModeSaaS:
		return false
	default:
		for _, svc := range s.config.Services {
			if svc.URL != "" {
				return false
			}
		}
		return true
	}
}

// buildAnalyzeContext formats project info for the Analyze prompt.
func (s *Scout) buildAnalyzeContext(target TargetInfo) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Project: %s\n", s.config.Project.Name)
	fmt.Fprintf(&b, "Base URL: %s\n", target.URL)

	if len(s.config.Services) > 0 {
		b.WriteString("\nServices:\n")
		for _, svc := range s.config.Services {
			fmt.Fprintf(&b, "- %s: %s (health: %s)\n", svc.Name, svc.URL, svc.Health)
		}
	}

	if len(s.config.Invariants) > 0 {
		b.WriteString("\nInvariants:\n")
		for _, inv := range s.config.Invariants {
			fmt.Fprintf(&b, "- [%s] %s (check: %s, assertion: %s)\n",
				inv.ID, inv.Description, inv.Check, inv.Assertion)
		}
	}

	if len(s.config.Databases) > 0 {
		b.WriteString("\nDatabases:\n")
		for _, db := range s.config.Databases {
			fmt.Fprintf(&b, "- %s\n", db.Name)
		}
	}

	// Include already-known endpoints as ground truth.
	model := s.buildModelFromConfig()
	if len(model.API.Endpoints) > 0 {
		b.WriteString("\nKnown endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (confidence: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}

	return b.String()
}

// mergeAIInference adds AI-discovered endpoints and pages to the model.
// Avoids duplicates with config-ground-truth entries.
func (s *Scout) mergeAIInference(model *project.ProjectModel, aiOut AnalyzeOutput) {
	existingEndpoints := make(map[string]bool)
	for _, ep := range model.API.Endpoints {
		existingEndpoints[ep.Method+" "+ep.Path] = true
	}

	for _, ep := range aiOut.Endpoints {
		key := ep.Method + " " + ep.Path
		if !existingEndpoints[key] {
			model.API.Endpoints = append(model.API.Endpoints, project.EndpointDef{
				Method:     ep.Method,
				Path:       ep.Path,
				Confidence: ep.Confidence,
			})
		}
	}

	existingPages := make(map[string]bool)
	for _, pg := range model.Navigation.Pages {
		existingPages[pg.Path] = true
	}

	for _, pg := range aiOut.Pages {
		if !existingPages[pg.Path] {
			model.Navigation.Pages = append(model.Navigation.Pages, project.PageDef{
				Path:       pg.Path,
				Confidence: pg.Confidence,
			})
		}
	}

	model.TechStack = append(model.TechStack, aiOut.TechStack...)
}

// buildPlanContext formats the ProjectModel for the Plan prompt.
func (s *Scout) buildPlanContext(model *project.ProjectModel) string {
	var b strings.Builder

	if len(model.API.Endpoints) > 0 {
		b.WriteString("API Endpoints:\n")
		for _, ep := range model.API.Endpoints {
			fmt.Fprintf(&b, "- %s %s (confidence: %.1f)\n", ep.Method, ep.Path, ep.Confidence)
		}
	}

	if len(model.Navigation.Pages) > 0 {
		b.WriteString("\nPages:\n")
		for _, pg := range model.Navigation.Pages {
			fmt.Fprintf(&b, "- %s (confidence: %.1f)\n", pg.Path, pg.Confidence)
		}
	}

	if len(model.InvariantHints) > 0 {
		b.WriteString("\nInvariants:\n")
		for _, inv := range model.InvariantHints {
			fmt.Fprintf(&b, "- [%s] %s\n", inv.ID, inv.Description)
		}
	}

	modelJSON, _ := json.Marshal(model)
	b.WriteString("\n## Raw Model\n")
	b.WriteString(string(modelJSON))

	return b.String()
}

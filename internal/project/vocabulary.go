package project

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Vocabulary is the directed-edge routing vocabulary for a WS protocol. It is
// the single source of truth for the dynamic test generator and (future)
// Scout context. Type is an edge label, not a primary key. Struct tags carry
// BOTH yaml (on-disk file) and json (extractor subprocess stdout) so the same
// type decodes from either source.
type Vocabulary struct {
	Source VocabSource `yaml:"source" json:"source"`
	Edges  []VocabEdge `yaml:"edges" json:"edges"`
	// HTTPRoutes is the mounted HTTP route surface (Hono-extracted). Kept
	// separate from Edges: WS delivery semantics do not apply; coverage
	// synthesizes one edge per route in requiredEdges (http_trigger pattern).
	HTTPRoutes []VocabHTTPRoute `yaml:"http_routes,omitempty" json:"http_routes,omitempty"`
	// UI is the declared browser display surface (spec 2026-08-26 §4): one
	// assertion = one coverage-denominator unit, compiled into deterministic
	// browser_flow cases. Nil when the project declares no UI surface.
	UI *VocabUI `yaml:"ui,omitempty" json:"ui,omitempty"`
}

// VocabUI declares the web UI test surface: where the UI is served, the
// locale assertion strings are written in, the actor whose http_login yields
// the injected JWT, and the display promises themselves.
type VocabUI struct {
	BaseURL string `yaml:"base_url" json:"base_url"`
	Locale  string `yaml:"locale" json:"locale"`
	// AuthActor names the actor whose credentials seed the browser session
	// (email/password credentials run the UI login; default web-actor).
	AuthActor string `yaml:"auth_actor,omitempty" json:"auth_actor,omitempty"`
	// LoginPath overrides the UI login endpoint (default /api/auth/login).
	LoginPath  string             `yaml:"login_path,omitempty" json:"login_path,omitempty"`
	Assertions []VocabUIAssertion `yaml:"assertions" json:"assertions"`
}

// VocabUIAssertion is one static display promise: after navigating Route,
// Target must satisfy Expectation within Timeout seconds. ID is the
// coverage-denominator unit (requiredEdges synthesizes one ui_assert edge).
type VocabUIAssertion struct {
	ID          string `yaml:"id" json:"id"`
	Route       string `yaml:"route" json:"route"`
	Target      string `yaml:"target" json:"target"`
	Expectation string `yaml:"expectation" json:"expectation"`
	Timeout     int    `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Unsupported bool   `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Reason      string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// VocabSource records where the vocabulary was extracted from.
type VocabSource struct {
	Files       []VocabFile `yaml:"files" json:"files"`
	ProtocolRef string      `yaml:"protocol_ref" json:"protocol_ref"`
}

// VocabFile is one source file the vocabulary was derived from.
type VocabFile struct {
	Path string `yaml:"path" json:"path"`
	Hash string `yaml:"hash" json:"hash"`
}

// VocabEdge is one directed message flow: a frame of Type leaves FromRole (or a
// DO-spontaneous null) bound for ToRole under Trigger. Guard is provenance only;
// the test generator executes off FromRole.
type VocabEdge struct {
	FromRole            string             `yaml:"from_role" json:"from_role"` // web | bridge | null
	ToRole              string             `yaml:"to_role" json:"to_role"`     // web | bridge
	Type                string             `yaml:"type" json:"type"`
	Trigger             string             `yaml:"trigger" json:"trigger"` // connect_web|connect_bridge|disconnect_bridge|message_handled|broadcast_endpoint
	Guard               string             `yaml:"guard,omitempty" json:"guard,omitempty"`
	Delivery            VocabDelivery      `yaml:"delivery" json:"delivery"`
	RouteField          string             `yaml:"route_field,omitempty" json:"route_field,omitempty"`
	OnMissingRoute      *VocabMissingRoute `yaml:"on_missing_route,omitempty" json:"on_missing_route,omitempty"`
	RequiresPresentRole string             `yaml:"requires_present_role,omitempty" json:"requires_present_role,omitempty"`
	SideEffects         []VocabSideEffect  `yaml:"side_effects,omitempty" json:"side_effects,omitempty"`
	Batch               *VocabBatch        `yaml:"batch,omitempty" json:"batch,omitempty"`
	Partial             bool               `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported         bool               `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source              VocabEdgeSource    `yaml:"source" json:"source"`
}

// VocabDelivery declares how a frame is distributed.
type VocabDelivery struct {
	Mode          string `yaml:"mode" json:"mode"` // broadcast_web | send_bridge_by_device | unicast_web
	ExcludeSender bool   `yaml:"exclude_sender,omitempty" json:"exclude_sender,omitempty"`
}

// VocabMissingRoute declares the reaction when a route_field target is absent.
type VocabMissingRoute struct {
	Kind string `yaml:"kind" json:"kind"` // send_error
	Code string `yaml:"code" json:"code"`
}

// VocabSideEffect is an out-of-band action triggered by an edge.
type VocabSideEffect struct {
	Kind      string   `yaml:"kind" json:"kind"` // notify_orchestrator | stuck_recovery
	WhenTypes []string `yaml:"when_types,omitempty" json:"when_types,omitempty"`
}

// VocabBatch declares a deferred flush window for batched edges.
type VocabBatch struct {
	WindowMs int    `yaml:"window_ms" json:"window_ms"`
	Key      string `yaml:"key" json:"key"`
}

// VocabEdgeSource locates the emit point(s) in the source file.
type VocabEdgeSource struct {
	Spans []VocabSpan `yaml:"spans" json:"spans"`
}

// VocabSpan is a half-open source line range.
type VocabSpan struct {
	Start int `yaml:"start" json:"start"`
	End   int `yaml:"end" json:"end"`
}

// VocabHTTPRoute is one mounted HTTP route. Identity is METHOD|Path. Path is
// the full normalized pattern (mount chain + route path); :param matches one
// segment, a trailing * matches one-or-more, ALL matches any method.
type VocabHTTPRoute struct {
	Method      string          `yaml:"method" json:"method"`
	Path        string          `yaml:"path" json:"path"`
	Mount       string          `yaml:"mount,omitempty" json:"mount,omitempty"`
	Partial     bool            `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported bool            `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source      VocabEdgeSource `yaml:"source" json:"source"`
}

// vocabRouteMethods is the closed method enum (ALL = Hono app.all).
var vocabRouteMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "OPTIONS": true, "HEAD": true, "ALL": true,
}

// ValidateVocabulary checks the HTTP route surface so a broken denominator
// cannot pass silently (same principle as claims).
func ValidateVocabulary(v *Vocabulary) error {
	for i, r := range v.HTTPRoutes {
		if !vocabRouteMethods[r.Method] {
			return fmt.Errorf("http_routes[%d]: method %q not in enum", i, r.Method)
		}
		if !strings.HasPrefix(r.Path, "/") || strings.Contains(r.Path, "//") {
			return fmt.Errorf("http_routes[%d]: path %q must start with / and contain no empty segments", i, r.Path)
		}
		segs := strings.Split(strings.Trim(r.Path, "/"), "/")
		for j, s := range segs {
			if strings.Contains(s, "*") && (s != "*" || j != len(segs)-1) {
				return fmt.Errorf("http_routes[%d]: * must be the lone final segment in %q", i, r.Path)
			}
			if s == ":" || (strings.HasPrefix(s, ":") && len(s) == 1) {
				return fmt.Errorf("http_routes[%d]: empty param name in %q", i, r.Path)
			}
		}
	}
	if v.UI != nil {
		if err := validateUI(v.UI); err != nil {
			return err
		}
	}
	return nil
}

// uiComparatorKnown reports whether an expectation string is one of the four
// browser_expect comparators (element_count>=N parses the suffix).
func uiComparatorKnown(c string) bool {
	if c == "text_present" || c == "text_absent" || c == "element_visible" {
		return true
	}
	return strings.HasPrefix(c, "element_count>=")
}

// validateUI enforces the display-promise contract: base_url + locale are
// required (locale pins assertion strings — the UI is i18n'd, so an unpinned
// locale is a flake factory), every non-exempt assertion needs id/route/
// target/expectation with a known comparator, ids are unique, and unsupported
// assertions must state why (same escape-hatch discipline as WS edges).
func validateUI(ui *VocabUI) error {
	if ui.BaseURL == "" {
		return fmt.Errorf("ui: base_url is required")
	}
	if ui.Locale == "" {
		return fmt.Errorf("ui: locale is required (assertion strings are locale-pinned)")
	}
	seen := map[string]bool{}
	for i, a := range ui.Assertions {
		if a.ID == "" {
			return fmt.Errorf("ui.assertions[%d]: id is required", i)
		}
		if seen[a.ID] {
			return fmt.Errorf("ui.assertions[%d]: duplicate id %q", i, a.ID)
		}
		seen[a.ID] = true
		if a.Unsupported {
			if a.Reason == "" {
				return fmt.Errorf("ui.assertions[%d] (%s): unsupported requires a reason", i, a.ID)
			}
			continue
		}
		if !strings.HasPrefix(a.Route, "/") {
			return fmt.Errorf("ui.assertions[%d] (%s): route must start with /", i, a.ID)
		}
		if a.Target == "" {
			return fmt.Errorf("ui.assertions[%d] (%s): target is required", i, a.ID)
		}
		if !uiComparatorKnown(a.Expectation) {
			return fmt.Errorf("ui.assertions[%d] (%s): unknown comparator %q", i, a.ID, a.Expectation)
		}
	}
	return nil
}

// LoadVocabulary reads and parses a vocab.yaml file.
func LoadVocabulary(path string) (*Vocabulary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("vocab: read %s: %w", path, err)
	}
	var v Vocabulary
	if err := yaml.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("vocab: parse %s: %w", path, err)
	}
	if err := ValidateVocabulary(&v); err != nil {
		return nil, fmt.Errorf("vocab: %s: %w", path, err)
	}
	return &v, nil
}

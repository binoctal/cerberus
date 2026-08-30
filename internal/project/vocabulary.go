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
	// HTTPAuthMiddlewares is the service-level list of middleware names that
	// authenticate a request. Cross-checked against per-route auth facts so
	// the generator can derive auth shapes (spec 2026-08-26 v2).
	HTTPAuthMiddlewares []string `yaml:"http_auth_middlewares,omitempty" json:"http_auth_middlewares,omitempty"`
	// UI is the declared browser display surface (spec 2026-08-26 §4): one
	// assertion = one coverage-denominator unit, compiled into deterministic
	// browser_flow cases. Nil when the project declares no UI surface.
	UI *VocabUI `yaml:"ui,omitempty" json:"ui,omitempty"`
	// HTTPRoleRoutes declares which protocol role's credential the HTTP
	// generators inject per path prefix (spec 2026-08-26 v2 §3: SUT facts
	// live in the vocab, not in generator code). Longest matching prefix
	// wins; HTTPDefaultRole covers everything else.
	HTTPRoleRoutes []VocabRoleRoute `yaml:"http_role_routes,omitempty" json:"http_role_routes,omitempty"`
	// HTTPDefaultRole is the fallback role for paths matching no
	// HTTPRoleRoutes prefix (used only when it carries a credential).
	HTTPDefaultRole string `yaml:"http_default_role,omitempty" json:"http_default_role,omitempty"`
}

// VocabRoleRoute maps a path prefix to the protocol role whose JWT the HTTP
// generators inject on routes under it. Shape-validated here; whether the
// role exists (and carries a CredentialRef) is checked at generation time,
// where the protocol is available.
type VocabRoleRoute struct {
	Prefix string `yaml:"prefix" json:"prefix"`
	Role   string `yaml:"role" json:"role"`
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

// VocabUIAssertion is one display promise: after navigating Route,
// Target must satisfy Expectation within Timeout seconds. ID is the
// coverage-denominator unit (requiredEdges synthesizes one ui_assert edge).
// A static promise needs no FromAPI; a protocol-coupled promise declares
// the API request whose captured values template into Target as
// {{case.<name>}} (spec §4 follow-up: "协议↔显示一致性").
type VocabUIAssertion struct {
	ID          string          `yaml:"id" json:"id"`
	Route       string          `yaml:"route" json:"route"`
	Target      string          `yaml:"target" json:"target"`
	Expectation string          `yaml:"expectation" json:"expectation"`
	Timeout     int             `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Unsupported bool            `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Reason      string          `yaml:"reason,omitempty" json:"reason,omitempty"`
	FromAPI     *VocabUIFromAPI `yaml:"from_api,omitempty" json:"from_api,omitempty"`
}

// VocabUIFromAPI is the protocol-side source of a coupled assertion: a GET
// against the service API whose captured response values (dot-paths, with
// "length:<path>" for array sizes) substitute into the assertion Target.
type VocabUIFromAPI struct {
	Method   string            `yaml:"method" json:"method"`
	Path     string            `yaml:"path" json:"path"`
	AuthRole string            `yaml:"auth_role,omitempty" json:"auth_role,omitempty"` // default "web"
	Capture  map[string]string `yaml:"capture" json:"capture"`
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
	Method string `yaml:"method" json:"method"`
	Path   string `yaml:"path" json:"path"`
	Mount  string `yaml:"mount,omitempty" json:"mount,omitempty"`
	// Middlewares names the route's middleware chain, outermost first.
	Middlewares []string `yaml:"middlewares,omitempty" json:"middlewares,omitempty"`
	// Auth is the resolved auth shape: required | none | unknown ("" = unset).
	Auth string `yaml:"auth,omitempty" json:"auth,omitempty"`
	// MinBody is the minimal JSON request body that satisfies validation,
	// keyed by field path; nil when the route takes no body.
	MinBody map[string]any `yaml:"min_body,omitempty" json:"min_body,omitempty"`
	// MinQuery is the minimal query-string parameters a GET needs to pass
	// handler-side validation (hand-curated live-probe knowledge — the
	// manual `if (!a) return 400` guard shape is not source-derivable;
	// preserved across re-extraction like the role map).
	MinQuery map[string]string `yaml:"min_query,omitempty" json:"min_query,omitempty"`
	// ParamSources maps each :param in Path to the list route whose captured
	// response yields a concrete value for it (param chaining).
	ParamSources map[string]VocabParamSource `yaml:"param_sources,omitempty" json:"param_sources,omitempty"`
	// ParamSourcesOff vetoes inference for the named params: re-extraction
	// never re-derives a chain for them (a hand-deleted unresolvable shape
	// must stay deleted, degrading the route to reachability).
	ParamSourcesOff []string        `yaml:"param_sources_off,omitempty" json:"param_sources_off,omitempty"`
	Partial         bool            `yaml:"partial,omitempty" json:"partial,omitempty"`
	Unsupported     bool            `yaml:"unsupported,omitempty" json:"unsupported,omitempty"`
	Source          VocabEdgeSource `yaml:"source" json:"source"`
}

// VocabParamSource chains a route param to a list-route response value: run
// Route (a GET list endpoint), then Pick the dot-path out of the response.
type VocabParamSource struct {
	Route string `yaml:"route" json:"route"` // e.g. "GET /api/devices"
	Pick  string `yaml:"pick" json:"pick"`   // dot-path, e.g. "0.id"
}

// vocabRouteMethods is the closed method enum (ALL = Hono app.all).
var vocabRouteMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "OPTIONS": true, "HEAD": true, "ALL": true,
}

// ValidateVocabulary checks the HTTP route surface so a broken denominator
// cannot pass silently (same principle as claims).
func ValidateVocabulary(v *Vocabulary) error {
	// routeSet indexes every declared path so param_sources can be checked
	// against the whole surface, not just routes seen so far.
	routeSet := map[string]bool{}
	for _, r := range v.HTTPRoutes {
		routeSet[r.Path] = true
	}
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
		switch r.Auth {
		case "", "required", "none", "unknown":
		default:
			return fmt.Errorf("http_routes[%d]: auth %q not in enum (required|none|unknown)", i, r.Auth)
		}
		for name, ps := range r.ParamSources {
			if !paramInPath(r.Path, name) {
				return fmt.Errorf("http_routes[%d]: param_sources key %q not a :param of %q", i, name, r.Path)
			}
			m, rp, ok := strings.Cut(ps.Route, " ")
			if !ok || m != "GET" || !routeSet[rp] {
				return fmt.Errorf("http_routes[%d]: param_sources[%q]: unresolved list route %q", i, name, ps.Route)
			}
		}
		for _, name := range r.ParamSourcesOff {
			if !paramInPath(r.Path, name) {
				return fmt.Errorf("http_routes[%d]: param_sources_off entry %q not a :param of %q", i, name, r.Path)
			}
			if _, hand := r.ParamSources[name]; hand {
				return fmt.Errorf("http_routes[%d]: param %q is both vetoed (param_sources_off) and sourced (param_sources)", i, name)
			}
		}
	}
	for i, rr := range v.HTTPRoleRoutes {
		if !strings.HasPrefix(rr.Prefix, "/") {
			return fmt.Errorf("http_role_routes[%d]: prefix %q must start with /", i, rr.Prefix)
		}
		if rr.Role == "" {
			return fmt.Errorf("http_role_routes[%d]: role is required", i)
		}
	}
	if v.UI != nil {
		if err := validateUI(v.UI); err != nil {
			return err
		}
	}
	return nil
}

// paramInPath reports whether name is a :param segment of path (the shared
// containment rule for param_sources keys and param_sources_off entries).
func paramInPath(path, name string) bool {
	return strings.Contains(path, "/"+name+"/") || strings.HasSuffix(path, name)
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
		if a.FromAPI != nil {
			// v1 coupling is read-only: a GET whose captured values template
			// into the selector. Mutating requests don't belong in the display
			// sweep (they'd perturb the very state being asserted).
			if a.FromAPI.Method != "GET" {
				return fmt.Errorf("ui.assertions[%d] (%s): from_api method must be GET in v1, got %q", i, a.ID, a.FromAPI.Method)
			}
			if !strings.HasPrefix(a.FromAPI.Path, "/") {
				return fmt.Errorf("ui.assertions[%d] (%s): from_api path must start with /", i, a.ID)
			}
			if len(a.FromAPI.Capture) == 0 {
				return fmt.Errorf("ui.assertions[%d] (%s): from_api requires a capture map (nothing would template into the target)", i, a.ID)
			}
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

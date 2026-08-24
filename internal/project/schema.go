package project

type Config struct {
	Project    ProjectMeta `yaml:"project"`
	Services   []Service   `yaml:"services,omitempty"`
	Actors     []Actor     `yaml:"actors,omitempty"`
	Databases  []Database  `yaml:"databases,omitempty"`
	Code       CodeConfig  `yaml:"code,omitempty"`
	Invariants []Invariant `yaml:"invariants,omitempty"`
	Settings   Settings    `yaml:"settings,omitempty"`
	// Claims is the project's claims ledger (.cerberus/claims.yaml), loaded
	// alongside the config; nil when absent. Runtime-only (not part of
	// project.yaml itself).
	Claims *ClaimsFile `yaml:"-"`
}

type ProjectMeta struct {
	Name string `yaml:"name"`
}

type Service struct {
	Name         string            `yaml:"name"`
	URL          string            `yaml:"url"`
	Health       string            `yaml:"health,omitempty"`
	Headers      map[string]string `yaml:"headers,omitempty"`
	PathPrefix   []string          `yaml:"path_prefix,omitempty"`
	BodyTemplate string            `yaml:"body_template,omitempty"`
	// Protocol optionally declares this service's WebSocket protocol facts.
	// When nil, the WS executor falls back to M0 behavior.
	Protocol *Protocol `yaml:"protocol,omitempty"`
	// ProtocolRef optionally names a standalone protocol description file
	// (.cerberus/protocols/<name>.yaml) loaded as this service's Protocol.
	// Mutually exclusive with Protocol (inline). Empty means use Protocol (or none).
	ProtocolRef string `yaml:"protocol_ref,omitempty"`
	// Vocabulary optionally declares this service's WS routing vocabulary
	// (directed-edge model), loaded alongside Protocol from
	// .cerberus/vocab/<protocol_ref>.vocab.yaml. Nil when no vocab file exists.
	Vocabulary *Vocabulary `yaml:"-"`
}

// Actor fidelity values. Empty is treated as FidelityEmulated (backwards
// compatible: actors without a process block are self-played by cerberus).
const (
	FidelityEmulated    = "emulated"
	FidelityRealProcess = "real-process"
)

type Actor struct {
	Name        string        `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Auth        *AuthFlow     `yaml:"auth,omitempty"`
	// Fidelity declares whether this actor is self-played by cerberus
	// (emulated, the default) or backed by a real external process
	// (real-process, requires a Process block). Drives the run-summary
	// fidelity watermark so "coverage 1.0" cannot silently mean
	// "self-played only".
	Fidelity string `yaml:"fidelity,omitempty"`
	// Process declares the external process behind a real-process actor.
	// Required iff Fidelity == FidelityRealProcess; forbidden otherwise.
	Process *ProcessSpec `yaml:"process,omitempty"`
	// GeneratedPathParams declares url-param -> generator for runtime-synthesized
	// path values (e.g. clientId: uuid). Unlike auth.path_params (captured from a
	// login response), these are generated locally at session setup — useful for
	// endpoints that expect a client-chosen id ({clientId}). Resolved values merge
	// into Credentials.PathParams and template {name} in the service URL at WS
	// connect. Non-secret; versionable in project.yaml. Supported generator: uuid.
	GeneratedPathParams map[string]string `yaml:"generated_path_params,omitempty"`
	Entry               string            `yaml:"entry,omitempty"`
	Service             string            `yaml:"service,omitempty"`
	// Replicas expands this actor into N identical real-process instances at
	// load time (names base-1..base-N). Per-instance variance is authored via
	// the {{actor.name}} template; only fidelity real-process actors may use
	// it. Absent/0 means exactly one actor (the declaration itself).
	Replicas int `yaml:"replicas,omitempty"`
}

// ProcessSpec declares an external process actor (fidelity: real-process).
// All SUT-specific facts live here in YAML; cerberus stays generic.
type ProcessSpec struct {
	// Workdir is the child process working directory (optional).
	Workdir string `yaml:"workdir,omitempty"`
	// Setup is a one-shot provisioning command run to completion before Start
	// (e.g. bridge pairing). Empty argv means no setup step.
	Setup []string `yaml:"setup,omitempty"`
	// Start is the long-running process argv (required).
	Start []string `yaml:"start"`
	// Env overrides the child environment. Values support the same templates
	// as Start entries ({{runtime.dir}} / {{actor.name}}).
	Env map[string]string `yaml:"env,omitempty"`
	// CaptureFile is a JSON file read after Setup; CaptureJSON maps
	// param name -> dot-path into that JSON (e.g. deviceId: devices.b1.deviceId).
	// Captured values merge into the actor's runtime PathParams.
	CaptureFile string `yaml:"capture_file,omitempty"`
	// CaptureJSON maps captured param names to dot-paths within CaptureFile.
	CaptureJSON map[string]string `yaml:"capture_json,omitempty"`
	// ReadyPattern is a regex on combined child stdout/stderr; the harness
	// waits for it before declaring the actor ready. Empty = no wait.
	ReadyPattern string `yaml:"ready_pattern,omitempty"`
	// ReadyTimeout bounds the readiness wait (Go duration string). Default 30s.
	ReadyTimeout string `yaml:"ready_timeout,omitempty"`
}

type CredentialRef struct {
	Email    string            `yaml:"email"`
	Password string            `yaml:"password"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	// Token is a static WS auth token (API key / dev backdoor) used when the actor
	// has no auth flow. A flow-resolved RawToken takes precedence. Loaded from YAML
	// (credentials.yaml, gitignored) — same secret hygiene as password.
	Token string `yaml:"token,omitempty"`
	// RawToken is the unformatted token cached at session setup (populated by
	// auth setup when the actor has an Auth flow). Runtime-only; not loaded
	// from YAML. Used by WS query/header/subprotocol auth injection.
	RawToken string `yaml:"-" json:"-"`
	// RawHTTPToken is the HTTP credential captured by the optional http_login
	// (distinct from RawToken, which is the WS credential). Populated at session
	// setup; read by the Steps runner to inject http_request Authorization headers.
	RawHTTPToken string `yaml:"-" json:"-"`
	// PathParams holds url-param -> value captured by the auth flow (F3).
	// Runtime-only; never loaded from YAML. Used at WS connect to resolve
	// {name} placeholders in the service URL.
	PathParams map[string]string `yaml:"-" json:"-"`
}

type Database struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
}

type CodeConfig struct {
	Root      string   `yaml:"root,omitempty"`
	Providers []string `yaml:"providers,omitempty"`
}

type Invariant struct {
	ID          string `yaml:"id"`
	Description string `yaml:"description"`
	Severity    string `yaml:"severity,omitempty"`
	Check       string `yaml:"check"`
	Assertion   string `yaml:"assertion,omitempty"`
}

// Mode values for Settings.Mode. Empty infers from services (backward
// compatible): no service URL → local codebase, else SaaS.
const (
	ModeLocal = "local"
	ModeSaaS  = "saas"
)

type Settings struct {
	Mode                string            `yaml:"mode,omitempty"`
	MaxDuration         string            `yaml:"max_duration,omitempty"`
	ConfidenceThreshold float64           `yaml:"confidence_threshold,omitempty"`
	AutoFix             string            `yaml:"auto_fix,omitempty"`
	AIBudget            AIBudget          `yaml:"ai_budget,omitempty"`
	CostAlerts          CostAlerts        `yaml:"cost_alerts,omitempty"`
	Models              Models            `yaml:"models,omitempty"`
	ToT                 ToTSettings       `yaml:"tot,omitempty"`
	Reflexion           ReflexionSettings `yaml:"reflexion,omitempty"`
	Coverage            CoverageSettings  `yaml:"coverage,omitempty"`
	Auth                AuthSettings      `yaml:"auth,omitempty"`

	// ReplanMaxRounds caps the in-session Examiner->Scout repair loop
	// (feature #3). 0 means "use the default" (resolved by config.ResolveReplanMaxRounds).
	ReplanMaxRounds int `yaml:"replan_max_rounds,omitempty"`
}

type AIBudget struct {
	SessionTotalTokens int    `yaml:"session_total_tokens,omitempty"`
	PerCallLimit       int    `yaml:"per_call_limit,omitempty"`
	Model              string `yaml:"model,omitempty"`
	BaseURL            string `yaml:"base_url,omitempty"`
}

type CostAlerts struct {
	WarnAtPct int `yaml:"warn_at_pct,omitempty"`
	StopAtPct int `yaml:"stop_at_pct,omitempty"`
}

// Models holds optional per-head model overrides.
// When a field is empty, the global AIBudget.Model is used.
type Models struct {
	Scout    string `yaml:"scout,omitempty"`
	Agent    string `yaml:"agent,omitempty"`
	Examiner string `yaml:"examiner,omitempty"`
	Critic   string `yaml:"critic,omitempty"`
}

// ToTSettings exposes Tree-of-Thought planning depth knobs. All optional;
// unset (zero) fields fall back to defaults (beam_width 3, generate_n 5,
// max_steps 3), so omitting the block preserves prior hardcoded behavior.
// See docs/configuration/tot.md for the cost trade-offs.
type ToTSettings struct {
	BeamWidth int `yaml:"beam_width,omitempty"`
	GenerateN int `yaml:"generate_n,omitempty"`
	MaxSteps  int `yaml:"max_steps,omitempty"`
}

// ReflexionSettings exposes cross-session memory recall knobs. All optional;
// unset fields fall back to defaults (episodic_limit 10, semantic_topk 5,
// semantic_threshold 0.3).
type ReflexionSettings struct {
	EpisodicLimit     int     `yaml:"episodic_limit,omitempty"`
	SemanticTopK      int     `yaml:"semantic_topk,omitempty"`
	SemanticThreshold float64 `yaml:"semantic_threshold,omitempty"`
}

// CoverageSettings configures the coverage contract tier and thresholds.
type CoverageSettings struct {
	Depth           string  `yaml:"depth,omitempty"`            // default "standard"
	LineThreshold   float64 `yaml:"line_threshold,omitempty"`   // default 0.65
	BranchThreshold float64 `yaml:"branch_threshold,omitempty"` // default 0.50
}

// AuthSettings configures session-only auth discovery. All optional; unset
// fields preserve prior behavior (no fallback).
type AuthSettings struct {
	DiscoverFallback bool `yaml:"discover_fallback,omitempty"` // opt-in: discover an AuthFlow in-memory at setup when an actor has no auth block
}

// ResolveCoverage fills defaults (called by DefaultConfig + config loaders).
func ResolveCoverage(c CoverageSettings) CoverageSettings {
	if c.Depth == "" {
		// Use literal string to avoid import cycle: project must not import contract package
		c.Depth = "standard" // contract.DepthStandard
	}
	if c.LineThreshold == 0 {
		c.LineThreshold = 0.65
	}
	if c.BranchThreshold == 0 {
		c.BranchThreshold = 0.50
	}
	return c
}

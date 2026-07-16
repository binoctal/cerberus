package project

type Config struct {
	Project    ProjectMeta `yaml:"project"`
	Services   []Service   `yaml:"services,omitempty"`
	Actors     []Actor     `yaml:"actors,omitempty"`
	Databases  []Database  `yaml:"databases,omitempty"`
	Code       CodeConfig  `yaml:"code,omitempty"`
	Invariants []Invariant `yaml:"invariants,omitempty"`
	Settings   Settings    `yaml:"settings,omitempty"`
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
}

type Actor struct {
	Name        string        `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Auth        *AuthFlow     `yaml:"auth,omitempty"`
	Entry       string        `yaml:"entry,omitempty"`
	Service     string        `yaml:"service,omitempty"`
}

type CredentialRef struct {
	Email    string            `yaml:"email"`
	Password string            `yaml:"password"`
	Headers  map[string]string `yaml:"headers,omitempty"`
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

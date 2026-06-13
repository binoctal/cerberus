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
	Name   string `yaml:"name"`
	URL    string `yaml:"url"`
	Health string `yaml:"health,omitempty"`
}

type Actor struct {
	Name        string        `yaml:"name"`
	Credentials CredentialRef `yaml:"credentials"`
	Entry       string        `yaml:"entry,omitempty"`
}

type CredentialRef struct {
	Email    string `yaml:"email"`
	Password string `yaml:"password"`
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

type Settings struct {
	MaxDuration         string     `yaml:"max_duration,omitempty"`
	ConfidenceThreshold float64    `yaml:"confidence_threshold,omitempty"`
	AutoFix             string     `yaml:"auto_fix,omitempty"`
	AIBudget            AIBudget   `yaml:"ai_budget,omitempty"`
	CostAlerts          CostAlerts `yaml:"cost_alerts,omitempty"`
	Models              Models     `yaml:"models,omitempty"`
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

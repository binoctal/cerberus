package architecture

import "time"

// IssueType represents the type of architecture issue
type IssueType string

const (
	OverEngineering        IssueType = "over_engineering"
	MissingScenario        IssueType = "missing_scenario_analysis"
	PrematureAbstraction   IssueType = "premature_abstraction"
	CircularDependency     IssueType = "circular_dependency"
	ViolatesSRP           IssueType = "violates_srp"
	ViolatesOCP           IssueType = "violates_ocp"
	ViolatesLSP           IssueType = "violates_lsp"
	ViolatesISP           IssueType = "violates_isp"
	ViolatesDIP           IssueType = "violates_dip"
	HighCoupling          IssueType = "high_coupling"
	LowCohesion           IssueType = "low_cohesion"
)

// Severity represents the severity level of an issue
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// ArchitectureIssue represents an architecture problem
type ArchitectureIssue struct {
	ID          string   // Unique identifier
	Type        IssueType // Type of issue
	Severity    Severity  // error | warning | info
	File        string   // File with the issue
	Line        int      // Line number (if applicable)
	Description string   // Issue description
	Rationale   string   // Why this is a problem
	Suggestion  string   // How to fix it
	Confidence  float64  // AI inference confidence (if applicable)
	Evidence    []string // Evidence (code snippets, metrics, etc.)
}

// ArchitectureReport contains analysis results
type ArchitectureReport struct {
	ProjectPath     string
	AnalyzedAt      time.Time
	Issues          []ArchitectureIssue
	Metrics         *ArchitectureMetrics
	Recommendations []string
	Summary         *ReportSummary
}

// ReportSummary provides a high-level summary
type ReportSummary struct {
	TotalIssues      int
	ErrorCount       int
	WarningCount     int
	InfoCount        int
	HealthScore      int // 0-100
	CategoryScores   map[string]int
}

// ArchitectureMetrics represents code quality metrics
type ArchitectureMetrics struct {
	// Complexity metrics
	TotalFiles      int
	TotalLines      int
	AvgLinesPerFile int
	MaxLinesPerFile int
	
	// Function metrics
	TotalFunctions     int
	AvgFunctionsPerFile int
	MaxParameters      int
	MaxNestingDepth    int
	
	// Dependency metrics
	TotalDependencies     int
	CircularDependencies  int
	
	// Abstraction metrics
	TotalInterfaces     int
	UnusedAbstractions  int
	SingleImplInterfaces int
	
	// SOLID violations
	SRPViolations int // Single Responsibility Principle
	OCPViolations int // Open/Closed Principle
	LSPViolations int // Liskov Substitution Principle
	ISPViolations int // Interface Segregation Principle
	DIPViolations int // Dependency Inversion Principle

	// Documentation metrics
	ADRFiles    int // Architecture Decision Records
	DesignDocs  int // Design specification documents
	PlanDocs    int // Implementation plan documents
}

// CategoryScores provides scores by category
func (r *ArchitectureReport) CalculateCategoryScores() {
	if r.Summary.CategoryScores == nil {
		r.Summary.CategoryScores = make(map[string]int)
	}
	
	// Complexity score (inverted from issues)
	complexityIssues := 0
	for _, issue := range r.Issues {
		if issue.Type == OverEngineering {
			complexityIssues++
		}
	}
	r.Summary.CategoryScores["complexity"] = max(0, 100-complexityIssues*10)
	
	// Simplicity score
	simplicityIssues := 0
	for _, issue := range r.Issues {
		if issue.Type == PrematureAbstraction || issue.Type == OverEngineering {
			simplicityIssues++
		}
	}
	r.Summary.CategoryScores["simplicity"] = max(0, 100-simplicityIssues*15)
	
	// Maintainability score
	maintainabilityIssues := 0
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			maintainabilityIssues += 2
		} else if issue.Severity == SeverityWarning {
			maintainabilityIssues++
		}
	}
	r.Summary.CategoryScores["maintainability"] = max(0, 100-maintainabilityIssues*5)
}

// CalculateHealthScore computes overall health score (0-100)
func (r *ArchitectureReport) CalculateHealthScore() int {
	if r.Summary == nil {
		r.Summary = &ReportSummary{}
	}
	
	r.CalculateCategoryScores()
	
	// Weighted average of category scores
	total := 0
	weight := 0
	
	for category, score := range r.Summary.CategoryScores {
		var w int
		switch category {
		case "complexity":
			w = 30
		case "simplicity":
			w = 40
		case "maintainability":
			w = 30
		default:
			w = 10
		}
		total += score * w
		weight += w
	}
	
	if weight == 0 {
		return 100
	}
	
	r.Summary.HealthScore = total / weight
	return r.Summary.HealthScore
}

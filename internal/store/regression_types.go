package store

import (
	"database/sql"
	"time"
)

// RegressionTest represents a regression test case
type RegressionTest struct {
	ID             int
	Name           string
	BugID          sql.NullString // Can be NULL
	Category       string         // complexity/abstraction/solid
	TestType       string         // positive/negative
	Description    sql.NullString // Can be NULL
	FilePath       sql.NullString // Can be NULL
	InterfaceName  sql.NullString // Can be NULL
	ExpectedResult string
	ActualResult   sql.NullString // Can be NULL
	Status         string         // pending/pass/fail/skip
	LastRun        sql.NullTime   // Can be NULL
	LastError      sql.NullString // Can be NULL
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Notes          sql.NullString // Can be NULL
}

// KnownIssue represents a known issue or false positive
type KnownIssue struct {
	ID                int
	IssueType         string // over_engineering/false_positive
	FilePath          string
	LineNumber        int
	Description       string
	IsFalsePositive   bool
	VerifiedBy        sql.NullString // Can be NULL
	VerifiedAt        sql.NullTime   // Can be NULL
	VerificationNotes sql.NullString // Can be NULL
	RelatedBugID      sql.NullString // Can be NULL
	FixCommit         sql.NullString // Can be NULL
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// AccuracyReport represents accuracy tracking data
type AccuracyReport struct {
	ID              int
	RunID           string
	Timestamp       time.Time
	ProjectPath     string
	TotalIssues     int
	TruePositives   int
	FalsePositives  int
	TrueNegatives   int
	ComplexityAcc   sql.NullFloat64 // Can be NULL
	AbstractionAcc  sql.NullFloat64 // Can be NULL
	SolidAcc        sql.NullFloat64 // Can be NULL
	OverallAccuracy float64
	AnalyzerVersion sql.NullString // Can be NULL
	CreatedAt       time.Time
}

// BugRecord represents a bug tracking record
type BugRecord struct {
	ID                int
	BugID             string
	Title             string
	Description       string
	Severity          string
	Category          string
	AffectedComponent string
	Status            string
	FixedInVersion    string
	RootCause         string
	RegressionTestID  int
	ReportedAt        time.Time
	FixedAt           time.Time
	CreatedAt         time.Time
}

// RegressionStore handles regression test operations
type RegressionStore struct {
	store *Store
}

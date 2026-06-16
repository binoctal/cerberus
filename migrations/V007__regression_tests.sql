-- Migration V007: Add regression testing tables
-- This migration adds tables for tracking regression tests, known issues, and accuracy history

-- Regression tests table
CREATE TABLE IF NOT EXISTS regression_tests (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    bug_id TEXT,
    category TEXT NOT NULL, -- complexity/abstraction/solid
    test_type TEXT NOT NULL, -- positive/should_detect or negative/should_not_detect
    description TEXT,

    -- Test data
    file_path TEXT,
    interface_name TEXT,
    expected_line_count INTEGER,
    expected_impl_count INTEGER,

    -- Execution results
    expected_result TEXT NOT NULL,
    actual_result TEXT,
    status TEXT NOT NULL DEFAULT 'pending', -- pending/pass/fail/skip
    last_run DATETIME,
    last_error TEXT,

    -- Metadata
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_regression_tests_category ON regression_tests(category);
CREATE INDEX IF NOT EXISTS idx_regression_tests_status ON regression_tests(status);
CREATE INDEX IF NOT EXISTS idx_regression_tests_bug_id ON regression_tests(bug_id);

-- Known issues/false positives table
CREATE TABLE IF NOT EXISTS known_issues (
    id INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Issue information
    issue_type TEXT NOT NULL, -- over_engineering/false_positive
    file_path TEXT NOT NULL,
    line_number INTEGER,
    description TEXT NOT NULL,

    -- Verification
    is_false_positive BOOLEAN NOT NULL DEFAULT 0,
    verified_by TEXT,
    verified_at DATETIME,
    verification_notes TEXT,

    -- Related information
    related_bug_id TEXT,
    fix_commit TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_known_issues_type ON known_issues(issue_type);
CREATE INDEX IF NOT EXISTS idx_known_issues_false_positive ON known_issues(is_false_positive);
CREATE INDEX IF NOT EXISTS idx_known_issues_file_path ON known_issues(file_path);

-- Accuracy reports table
CREATE TABLE IF NOT EXISTS accuracy_reports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id TEXT NOT NULL UNIQUE,

    -- Run information
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    project_path TEXT NOT NULL,

    -- Statistics
    total_issues INTEGER NOT NULL,
    true_positives INTEGER NOT NULL,
    false_positives INTEGER NOT NULL,
    true_negatives INTEGER NOT NULL,

    -- Category accuracy
    complexity_accuracy REAL,
    abstraction_accuracy REAL,
    solid_accuracy REAL,

    -- Overall accuracy
    overall_accuracy REAL NOT NULL,

    -- Version info
    analyzer_version TEXT,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_accuracy_reports_run_id ON accuracy_reports(run_id);
CREATE INDEX IF NOT EXISTS idx_accuracy_reports_timestamp ON accuracy_reports(timestamp);

-- Bug tracker table
CREATE TABLE IF NOT EXISTS bug_tracker (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    bug_id TEXT NOT NULL UNIQUE,

    title TEXT NOT NULL,
    description TEXT NOT NULL,
    severity TEXT NOT NULL, -- critical/major/minor

    -- Classification
    category TEXT NOT NULL, -- accuracy/performance/usability
    affected_component TEXT,

    -- Status
    status TEXT NOT NULL DEFAULT 'open', -- open/in_progress/fixed/closed
    fixed_in_version TEXT,

    -- Root cause
    root_cause TEXT,

    -- Related regression test
    regression_test_id INTEGER,

    -- Time tracking
    reported_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    fixed_at DATETIME,

    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_bug_tracker_status ON bug_tracker(status);
CREATE INDEX IF NOT EXISTS idx_bug_tracker_category ON bug_tracker(category);

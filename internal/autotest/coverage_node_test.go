package autotest

import (
	"testing"
)

func TestParseJestCoverage(t *testing.T) {
	provider := &NodeCoverageProvider{}

	jestJSON := `{
  "/path/to/file.js": {
    "statementMap": {
      "0": {"start": {"line": 1, "column": 0}, "end": {"line": 5, "column": 3}},
      "1": {"start": {"line": 2, "column": 4}, "end": {"line": 2, "column": 10}},
      "2": {"start": {"line": 3, "column": 4}, "end": {"line": 4, "column": 8}}
    },
    "s": {
      "0": 1,
      "1": 0,
      "2": 1
    }
  }
}`

	report, err := provider.parseJestCoverage([]byte(jestJSON))
	if err != nil {
		t.Fatalf("parseJestCoverage() error = %v", err)
	}

	// Check totals
	if report.TotalFuncs != 3 {
		t.Errorf("parseJestCoverage() TotalFuncs = %d, want 3", report.TotalFuncs)
	}

	// Check covered count
	if report.CoveredFuncs != 2 {
		t.Errorf("parseJestCoverage() CoveredFuncs = %d, want 2", report.CoveredFuncs)
	}

	// Check profile length
	if len(report.Profile) != 3 {
		t.Errorf("parseJestCoverage() Profile length = %d, want 3", len(report.Profile))
	}

	// Check that uncovered statement is marked correctly
	var uncovered *CoverageLine
	for _, line := range report.Profile {
		if line.Count == 0 {
			uncovered = &line
			break
		}
	}

	if uncovered == nil {
		t.Fatal("parseJestCoverage() no uncovered statement found")
	}

	if uncovered.File != "/path/to/file.js" {
		t.Errorf("uncovered file = %s, want /path/to/file.js", uncovered.File)
	}
}

func TestNodeCoverageProvider_Gaps(t *testing.T) {
	provider := &NodeCoverageProvider{}

	report := &CoverageReport{
		Profile: []CoverageLine{
			{File: "/path/to/file1.js", Start: 1, End: 5, Count: 1},
			{File: "/path/to/file2.js", Start: 10, End: 15, Count: 0},
			{File: "/path/to/file3.js", Start: 20, End: 25, Count: 1},
		},
		TotalFuncs:   3,
		CoveredFuncs: 2,
	}

	gaps := provider.Gaps(report)

	if len(gaps) != 1 {
		t.Fatalf("Gaps() length = %d, want 1", len(gaps))
	}

	if gaps[0].File != "/path/to/file2.js" {
		t.Errorf("Gaps()[0].File = %s, want /path/to/file2.js", gaps[0].File)
	}

	if gaps[0].Reason != ReasonZeroCover {
		t.Errorf("Gaps()[0].Reason = %s, want %s", gaps[0].Reason, ReasonZeroCover)
	}
}

func TestShouldSkipNodeFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"node_modules/package/index.js", true},
		{".git/config/file.js", true},
		{"dist/bundle.min.js", true},
		{"src/component.js", false},
		{"coverage/reporter.js", true},
		{"build/index.js", true},
		{"src/utils/helpers.js", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := shouldSkipNodeFile(tt.path)
			if got != tt.want {
				t.Errorf("shouldSkipNodeFile(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestNodeTestFilePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "src/api/users.js",
			expected: "src/api/users.test.js",
		},
		{
			input:    "lib/utils.js",
			expected: "lib/utils.test.js",
		},
		{
			input:    "component.jsx",
			expected: "component.test.jsx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nodeTestFilePath(tt.input)
			if got != tt.expected {
				t.Errorf("nodeTestFilePath(%s) = %s, want %s", tt.input, got, tt.expected)
			}
		})
	}
}

func TestExtractNodeFunction(t *testing.T) {
	source := []byte(`export function foo() {
  return "bar";
}

export class Baz {
  constructor() {
    this.value = 42;
  }
}`)

	tests := []struct {
		name     string
		funcName string
		wantPkg  string
	}{
		{
			name:     "extract with line number",
			funcName: "file.js:L2",
			wantPkg:  "file.js",
		},
		{
			name:     "extract without line number",
			funcName: "file.js",
			wantPkg:  "file.js",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, snippet := extractNodeFunction(source, tt.funcName)
			if pkg != tt.wantPkg {
				t.Errorf("extractNodeFunction() pkg = %s, want %s", pkg, tt.wantPkg)
			}
			if snippet == "" {
				t.Errorf("extractNodeFunction() snippet = empty, want non-empty")
			}
		})
	}
}

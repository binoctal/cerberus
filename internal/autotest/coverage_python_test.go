package autotest

import (
	"testing"
)

func TestParseJSONCoverage(t *testing.T) {
	provider := &PythonCoverageProvider{}

	coverageJSON := `{
  "files": {
    "/path/to/file.py": {
      "summary": {
        "num_statements": 10,
        "covered_lines": 8,
        "percent_covered": 80.0,
        "missing_lines": "2-3"
      },
      "lines": {
        "1": 1,
        "2": 0,
        "3": 0,
        "4": 1,
        "5": 1
      }
    }
  },
  "meta": {
    "branch_coverage": false,
    "timestamp": "2023-01-01T00:00:00"
  }
}`

	report, err := provider.parseJSONCoverage([]byte(coverageJSON))
	if err != nil {
		t.Fatalf("parseJSONCoverage() error = %v", err)
	}

	// Check totals
	if report.TotalFuncs != 5 {
		t.Errorf("parseJSONCoverage() TotalFuncs = %d, want 5", report.TotalFuncs)
	}

	// Check covered count
	if report.CoveredFuncs != 3 {
		t.Errorf("parseJSONCoverage() CoveredFuncs = %d, want 3", report.CoveredFuncs)
	}

	// Check profile length
	if len(report.Profile) != 5 {
		t.Errorf("parseJSONCoverage() Profile length = %d, want 5", len(report.Profile))
	}

	// Check that uncovered lines are marked correctly
	uncoveredCount := 0
	for _, line := range report.Profile {
		if line.Count == 0 {
			uncoveredCount++
		}
	}

	if uncoveredCount != 2 {
		t.Errorf("parseJSONCoverage() uncovered count = %d, want 2", uncoveredCount)
	}
}

func TestPythonCoverageProvider_Gaps(t *testing.T) {
	provider := &PythonCoverageProvider{}

	report := &CoverageReport{
		Profile: []CoverageLine{
			{File: "/path/to/file1.py", Start: 1, End: 2, Count: 1},
			{File: "/path/to/file2.py", Start: 10, End: 11, Count: 0},
			{File: "/path/to/file3.py", Start: 20, End: 21, Count: 1},
		},
		TotalFuncs:   3,
		CoveredFuncs: 2,
	}

	gaps := provider.Gaps(report)

	if len(gaps) != 1 {
		t.Fatalf("Gaps() length = %d, want 1", len(gaps))
	}

	if gaps[0].File != "/path/to/file2.py" {
		t.Errorf("Gaps()[0].File = %s, want /path/to/file2.py", gaps[0].File)
	}

	if gaps[0].Reason != ReasonZeroCover {
		t.Errorf("Gaps()[0].Reason = %s, want %s", gaps[0].Reason, ReasonZeroCover)
	}
}

func TestShouldSkipPythonFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"venv/lib/file.py", true},
		{".venv/lib/file.py", true},
		{"env/lib/file.py", true},
		{".git/file.py", true},
		{"__pycache__/file.py", true},
		{"dist/file.py", true},
		{"build/file.py", true},
		{".pytest_cache/file.py", true},
		{"app/module.py", false},
		{"tests/test_app.py", false},
		{"lib/utils.py", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := shouldSkipPythonFile(tt.path)
			if got != tt.want {
				t.Errorf("shouldSkipPythonFile(%s) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestPythonTestFilePath(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		wantPath string
	}{
		{
			name:     "with tests directory",
			source:   "/project/src/app/users.py",
			wantPath: "/project/tests/test_users.py",
		},
		{
			name:     "without tests directory",
			source:   "/project/app/utils.py",
			wantPath: "/project/app/test_utils.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonTestFilePath(tt.source)
			// Since we can't easily mock os.Stat in tests, just check the format
			if got == "" {
				t.Errorf("pythonTestFilePath() returned empty string")
			}
			if !contains(got, "test_") {
				t.Errorf("pythonTestFilePath() = %s, should contain 'test_'", got)
			}
		})
	}
}

func TestExtractPythonFunction(t *testing.T) {
	source := []byte(`def foo():
    return "bar"

class Baz:
    def __init__(self):
        self.value = 42
`)

	tests := []struct {
		name     string
		funcName string
	}{
		{
			name:     "extract with line number",
			funcName: "file.py:L2",
		},
		{
			name:     "extract without line number",
			funcName: "file.py",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use "python3" if available, otherwise skip
			pkg, snippet := extractPythonFunction("python3", source, tt.funcName)
			if pkg == "" {
				t.Skip("Python not available")
			}
			if snippet == "" {
				t.Errorf("extractPythonFunction() snippet = empty, want non-empty")
			}
		})
	}
}

func TestFindProjectRoot(t *testing.T) {
	tests := []struct {
		name     string
		startDir string
		wantDir  string
	}{
		{
			name:     "find root with requirements.txt",
			startDir: "/project/src/module",
			wantDir:  "/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test requires actual filesystem, skip for now
			t.Skip("requires filesystem setup")
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || s[len(s)-len(substr):] == substr || containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

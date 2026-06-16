# AutoTest Node/Python Providers Design

**Date:** 2026-06-15
**Status:** Design
**Author:** Cerberus Team

## Background

Cerberus currently supports AutoTest for Go projects only. The system runs Go tests with coverage, identifies uncovered code gaps, uses AI to generate tests, and validates them. This design extends AutoTest to support Node.js and Python projects following the same architecture.

## Goals

1. Add Node.js (Jest) support for coverage-driven test generation
2. Add Python (pytest + coverage.py) support
3. Maintain compatibility with existing Go provider
4. Follow existing patterns and interfaces
5. Support mixed-language repositories (mono-repos)

## Architecture

### Current Architecture (Go-only)

```
AutoTest
├── CoverageProvider interface
│   └── GoCoverageProvider (covers go test, parse cover.out)
└── TestGenerator interface
    └── GoTestGenerator (extracts function, generates table-driven test)
```

### Proposed Architecture

```
AutoTest
├── ProjectDetector interface
│   ├── GoProjectDetector
│   ├── NodeProjectDetector
│   └── PythonProjectDetector
├── CoverageProvider interface
│   ├── GoCoverageProvider (existing)
│   ├── NodeCoverageProvider (Jest JSON)
│   └── PythonCoverageProvider (coverage.py SQLite)
└── TestGenerator interface
    ├── GoTestGenerator (existing)
    ├── NodeTestGenerator (Jest describe/it)
    └── PythonTestGenerator (pytest fixtures)
```

### File Structure

```
internal/autotest/
├── autotest.go              # Core engine (existing)
├── provider.go              # Interfaces (existing)
├── types.go                 # Shared types (existing)
├── coverage_go.go           # Go provider (existing)
├── gen_go.go                # Go generator (existing)
├── detector.go              # NEW: Project detection
├── coverage_node.go         # NEW: Node provider
├── coverage_python.go       # NEW: Python provider
├── gen_node.go              # NEW: Node generator
├── gen_python.go            # NEW: Python generator
├── common.go                # NEW: Shared utilities
└── config.go                # NEW: Provider configurations
```

## Component Design

### 1. Project Detection

**Interface:**
```go
type ProjectDetector interface {
    Detect(projectDir string) (supported bool, confidence float64, toolPath string)
}

type ProjectType string
const (
    ProjectTypeGo     ProjectType = "go"
    ProjectTypeNode   ProjectType = "node"
    ProjectTypePython ProjectType = "python"
)
```

**Detection Logic:**

**GoProjectDetector:**
- Check for `go.mod`
- Verify `go` command available
- Confidence: 1.0 if both present

**NodeProjectDetector:**
- Check for `package.json`
- Check for `node_modules` directory
- Verify Jest in dependencies
- Check Jest executable in `node_modules/.bin/jest`
- Confidence: 1.0 if Jest installed, 0.7 if Node without Jest

**PythonProjectDetector:**
- Check for `requirements.txt`, `setup.py`, or `pyproject.toml`
- Find Python interpreter (venv > .venv > env > PATH)
- Verify `pytest` module available
- Verify `coverage` module available
- Confidence: 1.0 if all present, decreasing for missing components

**Detection Priority:** Go > Node > Python

### 2. Node.js Coverage Provider

**Coverage Configuration:**
```go
type CoverageConfig struct {
    TestCommand []string        // ["npm", "test"]
    CoverageArgs []string      // ["--", "--coverage", "--coverageReporters=json"]
    OutputPath   string         // "coverage/coverage-final.json"
    Timeout      time.Duration  // 5 minutes
    Env          []string       // ["NODE_ENV=test"]
}
```

**Execution Flow:**
1. Create output directory if needed
2. Run `npm test -- --coverage --coverageReporters=json`
3. Parse `coverage/coverage-final.json`
4. Map to `CoverageReport` structure

**Jest JSON Format:**
```json
{
  "/path/to/file.js": {
    "statementMap": {...},
    "s": {"0": 0, "1": 1},  // statement counts
    "functions": {...}
  }
}
```

**Gap Detection:**
- Extract zero-count statements
- Identify uncovered functions
- Map to `CoverageGap` with file and function name

### 3. Python Coverage Provider

**Coverage Configuration:**
```go
type PythonCoverageConfig struct {
    PythonCmd    string         // Path to Python interpreter
    TestCommand  []string       // ["coverage", "run", "-m", "pytest"]
    ReportCmd    []string       // ["coverage", "report", "--json"]
    DatabasePath string         // ".coverage" (SQLite)
    OutputPath   string         // "coverage.json"
    Timeout      time.Duration  // 5 minutes
}
```

**Execution Flow:**
1. Run `coverage run -m pytest` (executes tests with coverage)
2. Run `coverage report --json -o coverage.json`
3. Parse `coverage.json`
4. Optionally query `.coverage` SQLite database for detailed line data

**Gap Detection:**
- Extract zero-coverage lines from JSON report
- Identify functions with no coverage
- Map to `CoverageGap` with file and function name

### 4. Node.js Test Generator

**AST Extraction:**
- Use simplified regex-based extraction for phase 1 (no Babel dependency)
- Match patterns: `export function foo`, `export class Bar`, `export default`
- Future: Integrate Babel via subprocess for accurate parsing

**Pattern Matching:**
```go
// Simplified extraction for phase 1
patterns := []string{
    `export\s+(?:async\s+)?function\s+(\w+)`,
    `export\s+class\s+(\w+)`,
    `export\s+default\s+(?:async\s+)?function\s+(\w+)`,
    `export\s+default\s+class\s+(\w+)`,
}
```

**Test Generation:**
- System prompt: "You are a Jest test author. Emit a complete .test.js file using describe/it blocks."
- Input: Package name, function signature, implementation
- Output: Complete Jest test with nested describes and test cases

**Test Template:**
```javascript
describe('functionName', () => {
  it('should handle case 1', () => {
    // Test implementation
  });

  it('should handle edge case', () => {
    // Test implementation
  });
});
```

**File Naming:**
- `src/api/users.js` → `src/api/users.test.js`
- Preserves directory structure

### 5. Python Test Generator

**AST Extraction:**
- Use Python `ast` module via subprocess (standard library, no dependency)
- Extract function and class definitions
- Preserve class hierarchy for methods

**Subprocess Call:**
```python
import ast, sys, json
source = open(sys.argv[1]).read()
tree = ast.parse(source)
functions = []
for node in ast.walk(tree):
    if isinstance(node, ast.FunctionDef):
        functions.append({
            'name': node.name,
            'lineno': node.lineno,
            'class': parent_class_name  # if method
        })
print(json.dumps(functions))
```

**Test Generation:**
- System prompt: "You are a pytest test author. Emit a complete test_*.py file using fixtures and parametrize."
- Input: Module name, function signature, implementation
- Output: Complete pytest test with fixtures and parameterized cases

**Test Template:**
```python
import pytest

class TestClassName:
    @pytest.fixture
    def setup(self):
        # Setup logic
        pass

    @pytest.mark.parametrize("input,expected", [
        (1, 2),
        (3, 4),
    ])
    def test_method_name(self, input, expected):
        # Test implementation
        assert result == expected
```

**File Naming:**
- Check if `tests/` directory exists at project root
- If yes: `app/users.py` → `tests/test_users.py`
- If no: `app/users.py` → `app/test_users.py`

## Implementation Details

### Cross-Language Process Execution

**Subprocess Pattern:**
```go
func execWithTimeout(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
    if cmd.Timeout > 0 {
        ctx, cancel := context.WithTimeout(ctx, cmd.Timeout)
        defer cancel()
        cmd = exec.CommandContext(ctx, cmd.Args...)
    }

    cmd.Dir = cmd.Dir
    output, err := cmd.CombinedOutput()

    if ctx.Err() == context.DeadlineExceeded {
        return nil, fmt.Errorf("command timed out after %v", cmd.Timeout)
    }

    return output, err
}
```

**Process Cleanup:**
- Use `context.WithTimeout` for timeout control
- Use `context.WithCancel` for graceful shutdown
- Log command execution for debugging

### Error Handling Strategy

**Mixed Mode (as decided):**
- Test command failure → Terminate AutoTest (tests must pass before generating)
- Coverage parse failure → Skip gap, log warning, continue
- Test generation failure → Skip gap, record in report
- Test write failure → Record as failed, don't revert

**Example:**
```go
report, err := provider.RunCoverage(ctx, projectDir)
if err != nil {
    if strings.Contains(err.Error(), "test failed") {
        return nil, fmt.Errorf("autotest: tests failing, fix before generating")
    }
    // Parse error: log but try to continue with partial data
    p.logger.Warn("coverage parse partial", zap.Error(err))
}
```

### Provider Selection Logic

**Automatic Detection:**
```go
func DetectAndCreateProvider(projectDir string, driver *ai.Driver) (CoverageProvider, TestGenerator, error) {
    detectors := []ProjectDetector{
        &GoProjectDetector{},
        &NodeProjectDetector{},
        &PythonProjectDetector{},
    }

    for _, detector := range detectors {
        supported, confidence, toolPath := detector.Detect(projectDir)
        if supported && confidence >= 0.9 {
            return createProviderFromDetector(detector, driver, toolPath)
        }
    }

    return nil, nil, fmt.Errorf("no supported provider found")
}
```

**Fallback Strategy:**
- If multiple project types detected (mono-repo), prefer Go > Node > Python
- If no provider with confidence >= 0.9, return error
- User can override via `.cerberus/project.yaml` configuration

## Configuration

### Project-Level Override

Users can override auto-detection in `.cerberus/project.yaml`:

```yaml
autotest:
  provider: "node"  # Force Node provider
  config:
    test_command: ["npm", "run", "test:unit"]
    coverage_args: ["--", "--coverage"]
```

### Provider-Specific Settings

**Node:**
```yaml
autotest:
  node:
    jest_config: "jest.config.js"
    test_timeout: "10m"
```

**Python:**
```yaml
autotest:
  python:
    python_cmd: ".venv/bin/python"
    pytest_config: "pytest.ini"
    test_timeout: "8m"
```

## Testing Strategy

### Unit Tests

**Detector Tests:**
- Mock filesystem for different project types
- Test confidence scoring
- Test tool path resolution

**Provider Tests:**
- Mock subprocess execution
- Test coverage parsing with sample outputs
- Test error handling

**Generator Tests:**
- Mock LLM driver
- Test AST extraction
- Test prompt construction

### Integration Tests

**Test Fixtures:**
- Create sample Node project with Jest
- Create sample Python project with pytest
- Run full AutoTest flow

**Validation:**
- Verify coverage reports generated correctly
- Verify tests written to correct locations
- Verify coverage improves after tests added

### End-to-End Tests

**Self-Test:**
- Run AutoTest on cerberus codebase (Go)
- Run AutoTest on sample Node project
- Run AutoTest on sample Python project

## Open Questions

1. **Mono-repo Support:** Should AutoTest run per-project or repository-wide?
   - **Decision:** Per-project (respects existing boundary)

2. **Concurrent Test Execution:** Should providers support parallel test execution?
   - **Decision:** Not in phase 1 (keep simple)

3. **Custom Test Frameworks:** Support for Mocha, unittest, etc.?
   - **Decision:** Not in phase 1 (Jest and pytest only)

## Success Criteria

1. ✅ Node projects with Jest can run AutoTest
2. ✅ Python projects with pytest can run AutoTest
3. ✅ Generated tests follow idiomatic patterns (describe/it, fixtures)
4. ✅ Coverage increases after AutoTest run
5. ✅ Mixed-language repos work correctly
6. ✅ No regression in existing Go provider

## Timeline

- **Phase 1:** Implementation (2-3 days)
  - Project detection
  - Node provider
  - Python provider
- **Phase 2:** Testing (1-2 days)
  - Unit tests
  - Integration tests
- **Phase 3:** Documentation (1 day)
  - User guide
  - Examples

## Dependencies

**Node.js:**
- Requires `package.json` with Jest dependency
- Requires `npm` or `yarn` installed

**Python:**
- Requires `requirements.txt`, `setup.py`, or `pyproject.toml`
- Requires `pytest` and `coverage` packages
- Requires Python 3.7+ with `ast` module (standard library)

**External:**
- Python interpreter (system-provided)
- No additional dependencies for AST parsing (uses regex and Python stdlib)

# AutoTest for Node.js and Python Projects

## Overview

Cerberus AutoTest now supports **Node.js (Jest)** and **Python (pytest)** projects in addition to Go. AutoTest automatically:

1. Runs your existing test suite with coverage
2. Identifies uncovered code gaps
3. Uses AI to generate tests for those gaps
4. Validates generated tests
5. Persists only tests that improve coverage

## Quick Start

### Node.js Projects

**Prerequisites:**
- Node.js project with Jest installed
- `package.json` with Jest in `devDependencies` or `dependencies`

**Usage:**
```bash
# From your Node.js project root
cerberus run --dir . --goal "Generate tests for uncovered code" --auto-test-safety=dry-run
```

**What happens:**
1. Cerberus detects your Node.js project via `package.json`
2. Runs `npm test -- --coverage`
3. Parses `coverage/coverage-final.json`
4. Identifies uncovered functions and statements
5. AI generates `*.test.js` files for gaps
6. In dry-run mode, prints generated tests to console

### Python Projects

**Prerequisites:**
- Python project with pytest and coverage.py
- `requirements.txt`, `setup.py`, or `pyproject.toml` with pytest and coverage

**Usage:**
```bash
# From your Python project root
cerberus run --dir . --goal "Generate tests for uncovered code" --auto-test-safety=dry-run
```

**What happens:**
1. Cerberus detects your Python project via config files
2. Runs `coverage run -m pytest`
3. Parses coverage report (JSON or SQLite)
4. Identifies uncovered functions and methods
5. AI generates `test_*.py` files for gaps
6. In dry-run mode, prints generated tests to console

## Safety Modes

AutoTest supports four safety modes (same as Go projects):

### 1. Dry-Run Mode (Recommended First Step)
```bash
cerberus run --auto-test-safety=dry-run
```
- Runs coverage and generates tests
- **Does not write any files**
- Prints generated tests to console for review
- Perfect for previewing what AutoTest will do

### 2. Approve Mode (Interactive)
```bash
cerberus run --auto-test-safety=approve
```
- Prompts you before writing each generated test
- Shows preview and asks for confirmation
- You choose which tests to persist

### 3. Auto Mode
```bash
cerberus run --auto-test-safety=auto
```
- Generates and writes tests automatically
- Still validates tests before persisting
- Reverts tests that don't improve coverage

### 4. Off Mode
```bash
cerberus run --auto-test-safety=off
```
- Disables AutoTest phase
- Only runs Scout, Agent, and Examiner phases

## Project Detection

Cerberus automatically detects your project type:

**Node.js Detection:**
- ✅ `package.json` exists
- ✅ `node_modules/` directory exists
- ✅ Jest in dependencies
- Confidence: 1.0 if all present, 0.7-0.9 if partial

**Python Detection:**
- ✅ `requirements.txt`, `setup.py`, or `pyproject.toml` exists
- ✅ Python interpreter found (venv > .venv > env > PATH)
- ✅ `pytest` module available
- ✅ `coverage` module available
- Confidence: 1.0 if all present, decreasing for missing components

**Detection Priority:** Go > Node > Python

## Configuration

### Project-Level Configuration

You can configure AutoTest behavior in `.cerberus/project.yaml`:

```yaml
# .cerberus/project.yaml
project:
  name: "my-node-project"

settings:
  ai_budget:
    model: "glm-5.1"
    session_total_tokens: 30000
    per_call_limit: 10000

# AutoTest-specific settings
autotest:
  provider: "node"  # Force specific provider (optional)
  max_gaps: 5       # Maximum gaps to process per run

  # Node.js specific
  node:
    test_command: ["npm", "test"]
    jest_config: "jest.config.js"
    test_timeout: "10m"

  # Python specific
  python:
    python_cmd: ".venv/bin/python"
    pytest_config: "pytest.ini"
    test_timeout: "8m"
```

### Provider-Specific Settings

**Node.js (Jest):**
```yaml
autotest:
  node:
    test_command: ["npm", "run", "test:unit"]  # Custom test script
    coverage_args: ["--", "--coverage", "--coverageReporters=json"]
    output_path: "coverage/coverage-final.json"
    timeout: "10m"
    env:
      - "NODE_ENV=test"
```

**Python (pytest):**
```yaml
autotest:
  python:
    python_cmd: ".venv/bin/python"  # Path to Python interpreter
    test_command: ["pytest"]
    coverage_args: ["--cov", "--cov-report=json"]
    output_path: "coverage.json"
    timeout: "8m"
```

## Test File Organization

### Node.js (Jest)

**Convention:** `*.test.js` next to source file

```
src/
  api/
    users.js         → users.test.js
    utils.js         → utils.test.js
```

**Generated test structure:**
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

### Python (pytest)

**Convention:** `test_*.py` in `tests/` directory (preferred) or same directory

**With tests/ directory:**
```
project/
  src/
    app/
      users.py
  tests/
    test_users.py
```

**Without tests/ directory:**
```
src/
  app/
    users.py      → app/test_users.py
```

**Generated test structure:**
```python
import pytest

class TestFunctionName:
    @pytest.fixture
    def setup(self):
        # Setup logic
        pass

    @pytest.mark.parametrize("input,expected", [
        (1, 2),
        (3, 4),
    ])
    def test_function_name(self, input, expected):
        # Test implementation
        assert result == expected
```

## Examples

### Example 1: Node.js Express API

**Project structure:**
```
my-api/
  package.json
  src/
    routes/
      users.js
    controllers/
      userController.js
```

**Command:**
```bash
cerberus run --dir . --goal "Generate tests for API endpoints" --auto-test-safety=dry-run
```

**What it does:**
1. Detects Node.js project
2. Runs Jest with coverage
3. Finds uncovered routes/controllers
4. Generates `routes/users.test.js` and `controllers/userController.test.js`
5. Prints tests to console for review

### Example 2: Python Flask Application

**Project structure:**
```
my-flask-app/
  requirements.txt  # contains pytest, coverage
  app/
    __init__.py
    routes.py
    models.py
  tests/
    test_routes.py
```

**Command:**
```bash
cerberus run --dir . --goal "Generate tests for models and routes" --auto-test-safety=dry-run
```

**What it does:**
1. Detects Python project
2. Runs pytest with coverage
3. Finds uncovered functions
4. Generates test files (prefers `tests/` directory)
5. Prints tests to console

### Example 3: Mono-Repo with Multiple Languages

**Project structure:**
```
mono-repo/
  backend/          # Go project
    go.mod
  frontend/         # Node.js project
    package.json
  services/         # Python project
    requirements.txt
```

**Command (run from each subdirectory):**
```bash
# From frontend/
cerberus run --dir frontend --goal "Generate frontend tests"

# From services/
cerberus run --dir services --goal "Generate service tests"
```

## Best Practices

### 1. Start with Dry-Run
Always use `--auto-test-safety=dry-run` first to preview generated tests:
```bash
cerberus run --auto-test-safety=dry-run
```

### 2. Review Generated Tests
AI-generated tests are good starting points but may need:
- Better test data
- Additional edge cases
- Improved assertions

### 3. Run Your Test Suite First
AutoTest requires existing tests to pass:
```bash
# Node.js
npm test

# Python
pytest
```

Fix failing tests before running AutoTest.

### 4. Use Descriptive Goals
Provide clear goals for better test generation:
```bash
# Good
cerberus run --goal "Generate tests for authentication logic"

# Less specific
cerberus run --goal "Test everything"
```

### 5. Configure Timeouts
Large projects may need longer timeouts:
```yaml
autotest:
  node:
    test_timeout: "15m"  # Default is 5m
```

### 6. Virtual Environments for Python
Always use virtual environments for Python projects:
```bash
python -m venv .venv
source .venv/bin/activate  # On Windows: .venv\Scripts\activate
pip install pytest coverage
```

Configure cerberus to use it:
```yaml
autotest:
  python:
    python_cmd: ".venv/bin/python"
```

## Troubleshooting

### Node.js Issues

**Problem:** "Jest not found"
```bash
# Solution: Install Jest
npm install --save-dev jest
```

**Problem:** "node_modules not found"
```bash
# Solution: Install dependencies
npm install
```

**Problem:** "Coverage report not found"
```bash
# Solution: Check Jest configuration
cat jest.config.js
# Ensure --coverageReporters includes "json"
```

### Python Issues

**Problem:** "pytest not found"
```bash
# Solution: Install pytest
pip install pytest coverage
```

**Problem:** "Python interpreter not found"
```bash
# Solution: Create virtual environment
python -m venv .venv

# Or configure in .cerberus/project.yaml
autotest:
  python:
    python_cmd: "/usr/bin/python3"
```

**Problem:** "No coverage report"
```bash
# Solution: Check coverage.py installation
pip list | grep coverage
# Should show coverage 7.x.x
```

### General Issues

**Problem:** "Project type not detected"
- Ensure config files exist (`package.json`, `requirements.txt`, etc.)
- Check that test tools are installed
- Use verbose mode: `cerberus run --verbose`

**Problem:** "Generated tests fail"
- Review test output in dry-run mode
- Check AI model quality (use better models if needed)
- Manually fix and iterate

**Problem:** "Coverage doesn't improve"
- Ensure tests actually execute the uncovered code
- Check test fixtures and mock setup
- Verify test data covers actual code paths

## Advanced Usage

### Custom Test Commands

**Node.js:**
```yaml
autotest:
  node:
    test_command: ["npm", "run", "test:integration"]
```

**Python:**
```yaml
autotest:
  python:
    test_command: ["pytest", "tests/integration/"]
```

### Multiple Test Suites

Run AutoTest multiple times with different scopes:

```bash
# Generate unit tests
cerberus run --goal "Generate unit tests" --auto-test-safety=dry-run

# Generate integration tests
cerberus run --goal "Generate integration tests" --auto-test-safety=dry-run
```

### CI/CD Integration

**GitHub Actions example for Node.js:**
```yaml
- name: Run AutoTest (dry-run)
  run: |
    cerberus run --dir . --auto-test-safety=dry-run

- name: Review generated tests
  run: |
    echo "Check the logs above for generated tests"

- name: Apply approved tests
  if: github.event_name == 'pull_request'
  run: |
    cerberus run --auto-test-safety=approve
```

## Limitations

### Node.js
- Only supports Jest (not Mocha, Jasmine, etc.)
- Requires `package.json` and `node_modules`
- ES modules support depends on Jest configuration

### Python
- Only supports pytest (not unittest, nose, etc.)
- Requires pytest and coverage.py
- Virtual environment detection is heuristic-based

### General
- AI-generated tests are best-effort
- Complex code may need manual refinement
- Generated tests may not handle all edge cases
- Requires existing test suite to pass

## Future Enhancements

Planned for future releases:

1. **More Frameworks:**
   - Mocha for Node.js
   - unittest for Python
   - Additional Python test runners

2. **Better AST Analysis:**
   - Full Babel integration for Node.js
   - Type annotation support for Python

3. **Smarter Test Generation:**
   - Learn from existing tests
   - Generate test data based on types
   - Better edge case detection

4. **Mono-Repo Support:**
   - Run across entire mono-repo
   - Shared test utilities
   - Unified coverage reports

## Getting Help

- **Documentation:** `/docs/guide/autotest-node-python.md`
- **Design Spec:** `/docs/superpowers/specs/2026-06-15-autotest-node-python-providers-design.md`
- **Examples:** `/internal/autotest/testdata/`
- **Issues:** Report bugs on GitHub

## Summary

AutoTest for Node.js and Python brings the same coverage-driven test generation to non-Go projects:

- ✅ Automatic project detection
- ✅ Jest (Node.js) and pytest (Python) support
- ✅ AI-powered test generation
- ✅ Multiple safety modes
- ✅ Flexible configuration
- ✅ CI/CD integration

Start with `--auto-test-safety=dry-run` to preview what AutoTest can do for your project!

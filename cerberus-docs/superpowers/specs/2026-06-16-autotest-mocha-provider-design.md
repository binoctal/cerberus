# AutoTest Mocha Provider Design

**Date:** 2026-06-16
**Status:** Design
**Author:** Cerberus Team

## Background

Cerberus currently supports AutoTest for Go, Node.js (Jest), and Python projects. This design adds Mocha test framework support for Node.js projects, following the same architecture pattern as the existing Jest provider.

## Goals

1. Add Node.js (Mocha + nyc) support for coverage-driven test generation
2. Maintain compatibility with existing Go, Jest, and Python providers
3. Follow existing patterns and interfaces
4. Support intelligent test file organization (test/ directory vs same-directory)
5. Zero-configuration automatic detection with project-level overrides

## Architecture

### Detection Priority

```
Go > Node (Jest) > Node (Mocha) > Python
```

**Rationale:** Jest is the dominant Node.js test framework. Mocha is added as a secondary option for projects that don't use Jest.

### Component Overview

```
AutoTest
├── ProjectDetector interface
│   ├── GoProjectDetector (existing)
│   ├── NodeProjectDetector (Jest, existing)
│   ├── MochaProjectDetector (NEW)
│   └── PythonProjectDetector (existing)
├── CoverageProvider interface
│   ├── GoCoverageProvider (existing)
│   ├── NodeCoverageProvider (Jest, existing)
│   ├── MochaCoverageProvider (NEW)
│   └── PythonCoverageProvider (existing)
└── TestGenerator interface
    ├── GoTestGenerator (existing)
    ├── NodeTestGenerator (Jest, existing)
    ├── MochaTestGenerator (NEW)
    └── PythonTestGenerator (existing)
```

### Key Design Decisions

#### 1. Jest vs Mocha Detection Priority

**Decision:** Jest priority

**Rationale:**
- Minimal breaking changes for existing Jest users
- Jest has larger market share (~15M npm weekly downloads vs ~4M for Mocha)
- Simple implementation extending existing detector
- YAGNI principle - rare to have both Jest and Mocha in same project

**Detection Logic:**
```go
NodeProjectDetector:
  - Detects Jest only
  - Returns (false, 0, "") if no Jest found
  - Lets MochaProjectDetector handle non-Jest Node projects

MochaProjectDetector:
  - Only runs if NodeProjectDetector fails
  - Detects Mocha + nyc combination
```

#### 2. Coverage Tool Support

**Decision:** Mocha + nyc (Istanbul) only

**Rationale:**
- nyc is the de facto standard for Node.js coverage (>95% market share)
- Istanbul JSON format is identical to Jest format - can reuse parsing logic
- Clear documentation for users (`npm install --save-dev nyc`)
- c8 is newer and less mature

**Coverage Command:**
```bash
npm test -- --coverage --coverage-reporter=json
# or directly:
nyc mocha --reporter=json
```

**Output:** `coverage/coverage-final.json` (Istanbul JSON format)

#### 3. Test File Organization

**Decision:** Intelligent detection (test/ directory vs same-directory)

**Rationale:**
- Supports both modern Mocha projects (same-directory) and traditional projects (test/ directory)
- Zero configuration - automatic detection
- Precedent from Python provider (tests/ directory priority)
- Simple implementation

**Test File Path Logic:**
```go
func mochaTestFilePath(sourceFile string, projectDir string) string {
    // Check if test/ directory exists
    testDir := filepath.Join(projectDir, "test")
    if _, err := os.Stat(testDir); err == nil {
        // Use test/ directory mode
        // src/users.js → test/users.test.js
        relPath, _ := filepath.Rel(projectDir, sourceFile)
        return filepath.Join(testDir, filepath.Base(relPath)+".test.js")
    }

    // Same-directory mode (consistent with Jest)
    // src/users.js → src/users.test.js
    dir := filepath.Dir(sourceFile)
    base := filepath.Base(sourceFile)
    for _, ext := range []string{".jsx", ".tsx", ".ts", ".js"} {
        if strings.HasSuffix(base, ext) {
            name := strings.TrimSuffix(base, ext)
            return filepath.Join(dir, name+".test"+ext)
        }
    }
    return filepath.Join(dir, base+".test.js")
}
```

#### 4. Project Detection Confidence Levels

**Decision:** Hybrid detection with progressive confidence scoring

**Rationale:**
- Fault-tolerant for projects without explicit test scripts
- Keyword detection increases accuracy, reduces false positives
- Static checking only, no external command execution
- Progressive enhancement: quick screen → fine-grained check

**Detection Confidence Levels:**
```
Base check (0.5):
  - package.json exists

Loose check (0.7):
  - node_modules directory exists
  - No Jest detected

Strict check (0.9):
  - package.json contains "mocha" keyword
  - Or devDependencies contains "mocha"
  - Or devDependencies contains "nyc"

Validation check (1.0):
  - Found nyc or mocha executable
```

## Component Design

### 1. MochaProjectDetector

**Implementation:**
```go
type MochaProjectDetector struct{}

func (d *MochaProjectDetector) Type() ProjectType {
    return ProjectTypeMocha
}

func (d *MochaProjectDetector) Detect(projectDir string) (bool, float64, string) {
    // Base check (0.5): package.json exists
    pkgPath := filepath.Join(projectDir, "package.json")
    if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
        return false, 0, ""
    }

    // Loose check (0.7): node_modules exists, no Jest
    nodeModules := filepath.Join(projectDir, "node_modules")
    if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
        return false, 0.5, ""
    }

    // Check if Jest exists (don't interfere with Jest projects)
    data, err := os.ReadFile(pkgPath)
    if err != nil {
        return false, 0.6, ""
    }

    var pkg struct {
        DevDependencies map[string]string `json:"devDependencies"`
        Dependencies    map[string]string `json:"dependencies"`
        Scripts         struct {
            Test string `json:"test"`
        } `json:"scripts"`
    }
    if err := json.Unmarshal(data, &pkg); err != nil {
        return false, 0.6, ""
    }

    // If Jest exists, let Jest detector handle it
    hasJest := pkg.DevDependencies["jest"] != "" || pkg.Dependencies["jest"] != ""
    if hasJest {
        return false, 0, ""  // Not a Mocha project
    }

    // Strict check (0.9): mocha or nyc in dependencies
    hasMocha := pkg.DevDependencies["mocha"] != "" || pkg.Dependencies["mocha"] != ""
    hasNyc := pkg.DevDependencies["nyc"] != "" || pkg.Dependencies["nyc"] != ""
    hasMochaInScript := strings.Contains(pkg.Scripts.Test, "mocha")

    if hasMocha || hasNyc || hasMochaInScript {
        // Validation check (1.0): find executable
        nycPath := filepath.Join(nodeModules, ".bin", "nyc")
        if _, err := os.Stat(nycPath); err == nil {
            return true, 1.0, nycPath
        }

        mochaPath := filepath.Join(nodeModules, ".bin", "mocha")
        if _, err := os.Stat(mochaPath); err == nil {
            return true, 0.95, mochaPath
        }

        // No executable found but dependencies indicate Mocha project
        return true, 0.9, ""
    }

    // Node project but no Mocha detected
    return false, 0.7, ""
}
```

### 2. MochaCoverageProvider

**Implementation:**
```go
type MochaCoverageProvider struct {
    config *CoverageConfig
    logger *zap.Logger
}

func NewMochaCoverageProvider(cfg *CoverageConfig) *MochaCoverageProvider {
    return &MochaCoverageProvider{
        config: cfg,
        logger: zap.NewNop(),
    }
}

func (p *MochaCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
    if p.config == nil {
        return nil, fmt.Errorf("mocha coverage: config not set")
    }

    // Create output directory if needed
    outputDir := filepath.Dir(p.config.OutputPath)
    if outputDir != "." && outputDir != "" {
        if err := os.MkdirAll(filepath.Join(projectDir, outputDir), 0755); err != nil {
            return nil, fmt.Errorf("mocha coverage: create output dir: %w", err)
        }
    }

    // Build test command: npm test -- --coverage --coverage-reporter=json
    args := append(p.config.TestCommand, p.config.CoverageArgs...)
    cmd := exec.CommandContext(ctx, args[0], args[1:]...)
    cmd.Dir = projectDir
    if p.config.Env != nil {
        cmd.Env = append(os.Environ(), p.config.Env...)
    }

    p.logger.Info("running mocha coverage",
        zap.String("cmd", strings.Join(args, " ")),
        zap.String("dir", projectDir))

    // Run with timeout
    if p.config.Timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, p.config.Timeout)
        defer cancel()
        cmd = exec.CommandContext(ctx, args[0], args[1:]...)
        cmd.Dir = projectDir
        if p.config.Env != nil {
            cmd.Env = append(os.Environ(), p.config.Env...)
        }
    }

    // Run command - tests may fail but coverage report might still exist
    output, err := cmd.CombinedOutput()
    if err != nil {
        // Check if it was a timeout
        if ctx.Err() == context.DeadlineExceeded {
            return nil, fmt.Errorf("mocha coverage: timed out after %v", p.config.Timeout)
        }
        // Tests may have failed but coverage report might still exist
        p.logger.Warn("mocha tests failed but coverage might exist", zap.Error(err), zap.String("output", string(output)))
    }

    // Parse the JSON coverage report
    coveragePath := filepath.Join(projectDir, p.config.OutputPath)
    data, err := os.ReadFile(coveragePath)
    if err != nil {
        return nil, fmt.Errorf("mocha coverage: read coverage file: %w", err)
    }

    report, err := p.parseIstanbulCoverage(data)
    if err != nil {
        return nil, fmt.Errorf("mocha coverage: parse coverage: %w", err)
    }

    // Mark as pass if we got coverage data (even if tests failed)
    report.Pass = true

    p.logger.Info("mocha coverage complete",
        zap.Int("total_funcs", report.TotalFuncs),
        zap.Int("covered_funcs", report.CoveredFuncs))

    return report, nil
}

// parseIstanbulCoverage parses Istanbul JSON coverage format
// Istanbul JSON format is identical to Jest JSON format
func (p *MochaCoverageProvider) parseIstanbulCoverage(data []byte) (*CoverageReport, error) {
    // Reuse Jest coverage parsing logic
    var istanbulData JestCoverageJSON  // Same format!
    if err := json.Unmarshal(data, &istanbulData); err != nil {
        return nil, fmt.Errorf("unmarshal istanbul coverage: %w", err)
    }

    report := &CoverageReport{
        Profile: make([]CoverageLine, 0),
    }

    // Process each file's coverage
    for file, fileData := range istanbulData {
        for stmtIdx, count := range fileData.S {
            if stmtRange, ok := fileData.StatementMap[stmtIdx]; ok && stmtRange != nil {
                startLine := stmtRange.Start.Line
                endLine := stmtRange.End.Line

                report.Profile = append(report.Profile, CoverageLine{
                    File:  file,
                    Start: startLine,
                    End:    endLine,
                    Count:  count,
                })

                report.TotalFuncs++
                if count > 0 {
                    report.CoveredFuncs++
                }
            }
        }
    }

    return report, nil
}

func (p *MochaCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
    // Reuse Node provider's Gaps logic
    var gaps []CoverageGap

    for _, line := range report.Profile {
        if line.Count == 0 {
            gaps = append(gaps, CoverageGap{
                File:   line.File,
                Func:   fmt.Sprintf("%s:L%d", filepath.Base(line.File), line.Start),
                Reason: ReasonZeroCover,
            })
        }
    }

    return gaps
}

func (p *MochaCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
    var gaps []CoverageGap

    _ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
        if err != nil || info.IsDir() {
            return nil
        }

        // Skip non-JS files
        if !strings.HasSuffix(path, ".js") {
            return nil
        }

        // Skip test files themselves (.test.js and .spec.js)
        if strings.HasSuffix(path, ".test.js") || strings.HasSuffix(path, ".spec.js") {
            return nil
        }

        // Skip certain directories
        if shouldSkipNodeFile(path) {
            return nil
        }

        // Check for test file using intelligent path detection
        testFile := mochaTestFilePath(path, projectDir)
        if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
            gaps = append(gaps, CoverageGap{
                File:   path,
                Reason: ReasonNoTestFile,
            })
        }

        return nil
    })

    return gaps
}

func (p *MochaCoverageProvider) SetLogger(logger *zap.Logger) {
    p.logger = logger
}
```

### 3. MochaTestGenerator

**Implementation:**
```go
type MochaTestGenerator struct {
    driver *ai.Driver
    logger *zap.Logger
}

func NewMochaTestGenerator(driver interface{}) *MochaTestGenerator {
    var d *ai.Driver
    if v, ok := driver.(*ai.Driver); ok {
        d = v
    }
    return &MochaTestGenerator{
        driver: d,
        logger: zap.NewNop(),
    }
}

func (g *MochaTestGenerator) Generate(ctx context.Context, gap CoverageGap, source []byte) (TestFile, error) {
    // Extract function/class info (reuse Node logic)
    pkg, snippet := extractNodeFunction(source, gap.Func)

    prompt := g.buildPrompt(pkg, gap.File, snippet)

    // Try JSON response first
    var out struct {
        Test string `json:"test"`
    }

    err := g.driver.Decide(ctx, prompt, &out)
    if err == nil && strings.TrimSpace(out.Test) != "" {
        content := []byte(stripFences(out.Test))
        return TestFile{
            Path:    mochaTestFilePath(gap.File, ""),  // projectDir handled later
            Content: content,
        }, nil
    }

    // Fallback: use raw completion
    resp, rerr := g.driver.Client().Complete(ctx, llm.Request{
        Messages: []llm.Message{{Role: "user", Content: prompt}},
    })
    if rerr != nil {
        return TestFile{}, fmt.Errorf("mocha gen: decide %w, fallback %w", err, rerr)
    }

    content := []byte(stripFences(resp.Content))
    return TestFile{
        Path:    mochaTestFilePath(gap.File, ""),
        Content: content,
    }, nil
}

func (g *MochaTestGenerator) buildPrompt(pkg, file, snippet string) string {
    return `You are a Mocha test author. Emit a single complete .test.js file using describe/it blocks.
Use modern JavaScript (ES6+) and Mocha best practices.
Use the Node.js assert library for assertions (assert.equal, assert.strictEqual, assert.deepStrictEqual, etc.).
Include proper error handling tests.
Output ONLY the JavaScript source, no markdown fences.

Write a Mocha test file for this code.

File: %s
Package: %s

Source code:
%s

Return a complete .test.js file with:
- Proper require/import statements
- describe blocks grouping related tests
- it blocks for individual test cases
- assert assertions (not expect)
- Edge cases and error handling
- Clear test descriptions`
}

func (g *MochaTestGenerator) SetLogger(logger *zap.Logger) {
    g.logger = logger
}
```

**Test File Path Generation:**
```go
func mochaTestFilePath(sourceFile string, projectDir string) string {
    // If projectDir is provided, check for test/ directory
    if projectDir != "" {
        testDir := filepath.Join(projectDir, "test")
        if _, err := os.Stat(testDir); err == nil {
            // Use test/ directory mode
            relPath, _ := filepath.Rel(projectDir, sourceFile)
            return filepath.Join(testDir, filepath.Base(relPath)+".test.js")
        }
    }

    // Same-directory mode (consistent with Jest)
    dir := filepath.Dir(sourceFile)
    base := filepath.Base(sourceFile)

    // Try common extensions in order
    for _, ext := range []string{".jsx", ".tsx", ".ts", ".js"} {
        if strings.HasSuffix(base, ext) {
            name := strings.TrimSuffix(base, ext)
            return filepath.Join(dir, name+".test"+ext)
        }
    }

    // Fallback: just append .test.js
    return filepath.Join(dir, base+".test.js")
}
```

## Configuration

### Constants

```go
const (
    ProjectTypeGo     ProjectType = "go"
    ProjectTypeNode   ProjectType = "node"       // Jest
    ProjectTypeMocha  ProjectType = "mocha"      // NEW
    ProjectTypePython ProjectType = "python"
)
```

### Default Configuration

```go
func DefaultMochaCoverageConfig() *CoverageConfig {
    return &CoverageConfig{
        TestCommand:  []string{"npm", "test"},
        CoverageArgs: []string{"--", "--coverage", "--coverage-reporter=json"},
        OutputPath:   "coverage/coverage-final.json",
        Timeout:      5 * time.Minute,
        Env:          []string{"NODE_ENV=test"},
        ProjectType:  ProjectTypeMocha,
    }
}
```

### Project-level Override

```yaml
# .cerberus/project.yaml
autotest:
  coverage:
    test_command: ["nyc", "mocha"]
    output_path: "coverage/coverage-final.json"
    timeout: 10m
```

## Integration Points

### Updated DetectProjectType

```go
func DetectProjectType(projectDir string) (ProjectType, float64, error) {
    detectors := []ProjectDetector{
        &GoProjectDetector{},
        &NodeProjectDetector{},      // Jest only
        &MochaProjectDetector{},     // NEW
        &PythonProjectDetector{},
    }

    var bestType ProjectType
    var bestConfidence float64

    for _, detector := range detectors {
        supported, confidence, _ := detector.Detect(projectDir)
        if supported && confidence > bestConfidence {
            bestType = detector.Type()
            bestConfidence = confidence
        }
    }

    if bestConfidence >= 0.9 {
        return bestType, bestConfidence, nil
    }

    return "", 0, fmt.Errorf("no supported project type detected")
}
```

### Updated CreateProvider

```go
func CreateProvider(typ ProjectType, driver interface{}, cfg *CoverageConfig) (CoverageProvider, TestGenerator, error) {
    switch typ {
    case ProjectTypeGo:
        return nil, nil, fmt.Errorf("Go provider should use existing path")
    case ProjectTypeNode:
        return NewNodeCoverageProvider(cfg), NewNodeTestGenerator(driver), nil
    case ProjectTypeMocha:  // NEW
        if cfg == nil {
            cfg = DefaultMochaCoverageConfig()
        }
        return NewMochaCoverageProvider(cfg), NewMochaTestGenerator(driver), nil
    case ProjectTypePython:
        return NewPythonCoverageProvider(cfg), NewPythonTestGenerator(driver), nil
    default:
        return nil, nil, fmt.Errorf("unsupported project type: %s", typ)
    }
}
```

## File Structure

### New Files

```
internal/autotest/
├── coverage_mocha.go         # NEW: Mocha coverage provider
├── gen_mocha.go              # NEW: Mocha test generator
├── coverage_mocha_test.go    # NEW: Mocha provider tests
└── testdata/mocha_project/   # NEW: Mocha fixture
    ├── package.json
    ├── src/
    │   └── calculator.js
    ├── test/
    │   └── calculator.test.js
    └── coverage/
        └── coverage-final.json
```

### Modified Files

```
internal/autotest/
├── detector.go               # MODIFIED: Add MochaProjectDetector
├── config.go                 # MODIFIED: Add DefaultMochaCoverageConfig
└── types.go                  # MODIFIED: Add ProjectTypeMocha constant
```

## Error Handling

### Detection Errors

```go
// No package.json
if _, err := os.Stat(pkgPath); os.IsNotExist(err) {
    return false, 0, ""
}

// No node_modules (dependencies not installed)
if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
    return false, 0.5, ""
}

// Has Jest (not a Mocha project)
if hasJest {
    return false, 0, ""
}

// No Mocha or nyc
if !hasMocha && !hasNyc {
    return false, 0.7, ""
}
```

### Coverage Runtime Errors

```go
// Timeout
if ctx.Err() == context.DeadlineExceeded {
    return nil, fmt.Errorf("mocha coverage: timed out after %v", p.config.Timeout)
}

// Tests failed but coverage exists
if err != nil {
    p.logger.Warn("mocha tests failed but coverage might exist", zap.Error(err))
    // Continue to parse coverage file
}

// Coverage file missing
if err := os.ReadFile(coveragePath); err != nil {
    return nil, fmt.Errorf("mocha coverage: read coverage file: %w", err)
}
```

### Test Generation Errors

```go
// AI generation failure
if err := g.driver.Decide(ctx, prompt, &out); err != nil {
    // Fallback to raw completion
    resp, rerr := g.driver.Client().Complete(ctx, llm.Request{...})
    if rerr != nil {
        return TestFile{}, fmt.Errorf("mocha gen: decide %w, fallback %w", err, rerr)
    }
}
```

## Testing Strategy

### Unit Tests

**detector_test.go additions:**
```go
func TestMochaProjectDetector_Detect(t *testing.T) {
    detector := &MochaProjectDetector{}

    tests := []struct {
        name          string
        setup         func() (string, func())
        wantSupported bool
        wantConfidence float64
    }{
        {
            name: "Mocha + nyc project",
            setup: func() (string, func()) {
                tmpDir := t.TempDir()
                // Create package.json with mocha and nyc
                pkgJson := filepath.Join(tmpDir, "package.json")
                pkgContent := `{"devDependencies": {"mocha": "^10.0.0", "nyc": "^15.0.0"}}`
                os.WriteFile(pkgJson, []byte(pkgContent), 0644)
                // Create node_modules/.bin/nyc
                nodeModules := filepath.Join(tmpDir, "node_modules", ".bin")
                os.MkdirAll(nodeModules, 0755)
                nycPath := filepath.Join(nodeModules, "nyc")
                os.WriteFile(nycPath, []byte("#!/bin/sh\n"), 0755)
                return tmpDir, func() {}
            },
            wantSupported: true,
            wantConfidence: 1.0,
        },
        // ... more test cases
    }
    // ... test implementation
}
```

**coverage_mocha_test.go:**
```go
func TestMochaCoverageProvider_RunCoverage(t *testing.T)
func TestMochaCoverageProvider_Gaps(t *testing.T)
func TestMochaCoverageProvider_NoTestFileGaps(t *testing.T)
func TestMochaTestFilePath(t *testing.T)
func TestExtractMochaFunction(t *testing.T)
```

### Integration Tests

**Fixture Structure:**
```json
{
  "name": "mocha-fixture",
  "version": "1.0.0",
  "devDependencies": {
    "mocha": "^10.0.0",
    "nyc": "^15.0.0"
  },
  "scripts": {
    "test": "nyc mocha"
  }
}
```

**E2E Test:**
```go
func TestMochaAutoTest_E2E(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping E2E test in short mode")
    }

    fixtureDir := "testdata/mocha_project"
    typ, confidence, err := DetectProjectType(fixtureDir)
    assert.Equal(t, ProjectTypeMocha, typ)
    assert.GreaterOrEqual(t, confidence, 0.9)
    assert.NoError(t, err)

    // Create provider and generator
    driver := &ai.Driver{...}  // Mock driver
    provider, generator, err := CreateProvider(typ, driver, nil)
    assert.NoError(t, err)

    // Run coverage
    ctx := context.Background()
    report, err := provider.RunCoverage(ctx, fixtureDir)
    assert.NoError(t, err)
    assert.True(t, report.Pass)

    // Generate gaps
    gaps := provider.Gaps(report)
    assert.NotEmpty(t, gaps)

    // Generate test
    source, _ := os.ReadFile(filepath.Join(fixtureDir, "src", "calculator.js"))
    testFile, err := generator.Generate(ctx, gaps[0], source)
    assert.NoError(t, err)
    assert.NotEmpty(t, testFile.Content)
}
```

### Test Coverage Goals

- Unit test coverage: >80%
- Integration test coverage: All main code paths
- E2E test: Complete workflow (detect → coverage → generate)

## Backward Compatibility

### Non-breaking Changes

1. **Jest projects unchanged** - Detection priority ensures Jest projects continue using JestProvider
2. **Go provider unchanged** - No modifications to Go coverage logic
3. **Python provider unchanged** - No modifications to Python coverage logic
4. **Existing tests pass** - All existing unit and integration tests continue to pass

### Migration Path

**Existing Node + Jest projects:**
- Continue using JestProvider automatically
- No action required

**Existing Node + Mocha projects:**
- Automatically enable MochaProvider
- Requires nyc for coverage (new dependency)
- Optional: configure test command in project.yaml

**Mixed projects:**
```yaml
# .cerberus/project.yaml
autotest:
  provider: "mocha"  # or "jest"
  coverage:
    test_command: ["npm", "test"]
```

## Documentation

### User Guide Updates

Add section to `docs/guide/autotest-node-python.md`:

```markdown
## Mocha Support

Cerberus supports Mocha test framework with nyc coverage tool.

### Requirements

- Node.js project with Mocha
- nyc (Istanbul) for coverage: `npm install --save-dev nyc mocha`

### Configuration

**package.json:**
```json
{
  "scripts": {
    "test": "nyc mocha"
  },
  "devDependencies": {
    "mocha": "^10.0.0",
    "nyc": "^15.0.0"
  }
}
```

**Output:** `coverage/coverage-final.json`

### Test File Organization

Cerberus automatically detects your test file structure:

- **test/ directory** (traditional): `src/users.js` → `test/users.test.js`
- **Same-directory** (modern): `src/users.js` → `src/users.test.js`

### Example

```bash
cerberus run --dir . --goal "Generate tests for uncovered code" --auto-test-safety=dry-run
```
```

### README Updates

Update AutoTest section:
```markdown
- **AutoTest** — Coverage-driven test generation for Go, Node.js (Jest and Mocha), and Python (pytest) with AI
```

## Success Criteria

1. ✅ Mocha projects detected with confidence ≥0.9
2. ✅ Coverage reports parsed correctly (Istanbul JSON)
3. ✅ Test files generated in correct location (test/ vs same-directory)
4. ✅ AI-generated tests use Mocha syntax (describe/it + assert)
5. ✅ All existing tests pass (Go, Jest, Python)
6. ✅ Unit test coverage >80%
7. ✅ E2E test validates complete workflow
8. ✅ Documentation updated (user guide + README)

## Implementation Phases

### Phase 1: Core Implementation
- [ ] Add ProjectTypeMocha constant
- [ ] Implement MochaProjectDetector
- [ ] Implement MochaCoverageProvider
- [ ] Implement MochaTestGenerator
- [ ] Update DetectProjectType
- [ ] Update CreateProvider

### Phase 2: Testing
- [ ] Unit tests (detector, provider, generator)
- [ ] Integration tests (mocha_project fixture)
- [ ] E2E test (complete workflow)
- [ ] Test coverage verification

### Phase 3: Documentation
- [ ] Update user guide (Mocha section)
- [ ] Update README (AutoTest features)
- [ ] Update CHANGELOG (v0.9.0 entry)

### Phase 4: Validation
- [ ] Run all existing tests (ensure no regression)
- [ ] Test on real Mocha project (if available)
- [ ] Code review and refinements

## Future Enhancements (Out of Scope)

1. **c8 support** - V8 native coverage (faster but less mature)
2. **Custom reporters** - Support for alternative coverage formats
3. **TypeScript Mocha** - Enhanced .ts/.tsx test generation
4. **BDD syntax** - Support for Mocha's BDD interface (given/when/then)
5. **Async hooks** - Improved async/await test generation

## References

- [Mocha Documentation](https://mochajs.org/)
- [nyc (Istanbul) Documentation](https://github.com/istanbuljs/nyc)
- [Istanbul JSON Coverage Format](https://github.com/istanbuljs/istanbuljs/tree/master/packages/istanbul-coverage-to-json)
- Existing Jest provider implementation
- Existing Python provider implementation

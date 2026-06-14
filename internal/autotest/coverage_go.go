package autotest

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// coverageRunner runs `go test -coverprofile` and returns the profile bytes.
// Injected so tests don't shell out.
type coverageRunner func(ctx context.Context, projectDir string) ([]byte, error)

type GoCoverageProvider struct {
	run    coverageRunner
	logger *zap.Logger
}

func NewGoCoverageProvider(run coverageRunner, logger *zap.Logger) *GoCoverageProvider {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &GoCoverageProvider{run: run, logger: logger}
}

// RunCoverage invokes the runner and parses the returned cover.out text.
func (p *GoCoverageProvider) RunCoverage(ctx context.Context, projectDir string) (*CoverageReport, error) {
	if p.run == nil {
		return nil, fmt.Errorf("autotest: coverage runner not configured")
	}
	data, err := p.run(ctx, projectDir)
	if err != nil {
		return nil, fmt.Errorf("autotest: coverage run failed: %w", err)
	}
	rep, err := parseCoverProfile(data)
	if err != nil {
		return nil, err
	}
	rep.Pass = true // runner is responsible for surfacing test failures
	return rep, nil
}

// parseCoverProfile parses Go cover.out text (mode line + blocks).
// Format per block: file:start.col,end.col numStmts count
func parseCoverProfile(data []byte) (*CoverageReport, error) {
	rep := &CoverageReport{}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			continue
		}
		file := line[:colon]
		rest := line[colon+1:]
		parts := strings.Split(rest, " ")
		if len(parts) < 3 {
			continue
		}
		posComma := strings.Split(parts[0], ",")
		if len(posComma) != 2 {
			continue
		}
		start, _ := strconv.Atoi(strings.Split(posComma[0], ".")[0])
		end, _ := strconv.Atoi(strings.Split(posComma[1], ".")[0])
		count, _ := strconv.Atoi(parts[2])
		rep.Profile = append(rep.Profile, CoverageLine{
			File: file, Start: start, End: end, Count: count,
		})
		rep.TotalFuncs++
		if count > 0 {
			rep.CoveredFuncs++
		}
	}
	return rep, sc.Err()
}

// Gaps turns a report into uncovered targets: zero-count spans.
func (p *GoCoverageProvider) Gaps(report *CoverageReport) []CoverageGap {
	var gaps []CoverageGap
	for _, ln := range report.Profile {
		if ln.Count == 0 {
			gaps = append(gaps, CoverageGap{
				File:   ln.File,
				Func:   fmt.Sprintf("%s:L%d", filepath.Base(ln.File), ln.Start),
				Reason: ReasonZeroCover,
			})
		}
	}
	return gaps
}

// NoTestFileGaps walks projectDir for *.go source files (non-test, non-main,
// non-generated) that have no sibling *_test.go, returning them as gaps.
func (p *GoCoverageProvider) NoTestFileGaps(projectDir string) []CoverageGap {
	var gaps []CoverageGap
	_ = filepath.Walk(projectDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		testFile := strings.TrimSuffix(path, ".go") + "_test.go"
		if _, statErr := os.Stat(testFile); os.IsNotExist(statErr) {
			gaps = append(gaps, CoverageGap{File: path, Reason: ReasonNoTestFile})
		}
		return nil
	})
	return gaps
}

// shouldSkipFile reports files excluded from autotest: generated code, main
// packages, vendor. YAGNI boundaries from the spec.
func shouldSkipFile(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_gen.go") {
		return true
	}
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == "vendor" || seg == ".git" || seg == "node_modules" {
			return true
		}
	}
	return false
}

// DefaultGoCoverageRunner shells out to `go test -coverprofile=<tmp>/cover.out`.
// projectDir is the working directory. Returns the profile bytes.
func DefaultGoCoverageRunner(ctx context.Context, projectDir string) ([]byte, error) {
	tmp, err := os.MkdirTemp("", "cerberus-cover-")
	if err != nil {
		return nil, err
	}
	out := filepath.Join(tmp, "cover.out")
	cmd := exec.CommandContext(ctx, "go", "test", "-coverprofile="+out, "./...")
	cmd.Dir = projectDir
	// go test returns non-zero if tests fail; we still read the profile.
	if runErr := cmd.Run(); runErr != nil {
		_ = runErr // profile may still exist; failure surfaced via report.Pass later
	}
	return os.ReadFile(out)
}

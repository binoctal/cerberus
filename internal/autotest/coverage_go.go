package autotest

import (
	"bufio"
	"context"
	"fmt"
	"os"
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

// os import retained for file helpers added in later tasks.
var _ = os.ReadFile

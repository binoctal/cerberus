package smoke

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/binoctal/cerberus/internal/ai"
	"github.com/binoctal/cerberus/internal/head/examiner"
	"github.com/binoctal/cerberus/internal/head/scout"
	"github.com/binoctal/cerberus/internal/llm"
	"github.com/binoctal/cerberus/internal/project"
	"github.com/binoctal/cerberus/internal/store"
)

// SelfTestResult holds the outcome of a self-test run.
type SelfTestResult struct {
	OK     bool
	Checks []string
}

// RunSelfTest exercises the core LLM→driver→structured-parse pipeline with a
// mock client and verifies the store/scout/examiner components initialize. It
// is deterministic, hits no network, and is safe to run in CI as a binary
// health check. It does not depend on migration files on disk.
func RunSelfTest(ctx context.Context) (*SelfTestResult, error) {
	res := &SelfTestResult{}

	mockClient := llm.NewMockClient(map[string]string{
		"default": `{"status":"pass","existence_confidence":0.9,"correctness_confidence":0.9,"reasoning":"selftest ok"}`,
	})
	driver := ai.NewDriver(mockClient, ai.NewTokenBudget(10000, 1000))

	var verdict examiner.JudgeResult
	if err := driver.Decide(ctx, "selftest", &verdict); err != nil {
		return res, fmt.Errorf("llm roundtrip: %w", err)
	}
	if verdict.Status != examiner.StatusPass {
		return res, fmt.Errorf("llm roundtrip: unexpected status %q", verdict.Status)
	}
	res.Checks = append(res.Checks, "mock LLM → driver → structured parse OK")

	s, err := store.New(":memory:")
	if err != nil {
		return res, fmt.Errorf("store: %w", err)
	}
	defer func() { _ = s.Close() }()

	cfg := project.DefaultConfig()
	scout.NewScout(driver, s, &cfg, zap.NewNop())
	examiner.NewJudge(driver, nil, examiner.DefaultExaminerConfig())
	res.Checks = append(res.Checks, "store + scout + examiner initialize OK")

	res.OK = true
	return res, nil
}

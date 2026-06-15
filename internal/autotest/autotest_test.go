package autotest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type stubProvider struct{ pass bool }

func (s stubProvider) RunCoverage(_ context.Context, _ string) (*CoverageReport, error) {
	return &CoverageReport{Pass: s.pass, CoveredFuncs: 5, TotalFuncs: 10}, nil
}
func (s stubProvider) Gaps(_ *CoverageReport) []CoverageGap {
	return []CoverageGap{{File: "a.go", Func: "F", Reason: ReasonZeroCover}}
}

type stubGen struct{ content string }

func (g stubGen) Generate(_ context.Context, _ CoverageGap, _ []byte) (TestFile, error) {
	return TestFile{Path: "a_test.go", Content: []byte(g.content)}, nil
}

type memoryWriter struct {
	written  map[string][]byte
	reverted []string
}

func (m *memoryWriter) Write(tf TestFile) error {
	if m.written == nil {
		m.written = map[string][]byte{}
	}
	m.written[tf.Path] = tf.Content
	return nil
}
func (m *memoryWriter) Revert(path string) error {
	delete(m.written, path)
	m.reverted = append(m.reverted, path)
	return nil
}

type allowGate struct{}

func (allowGate) Request(_ context.Context, _ string, _ []string, _ string) (bool, error) {
	return true, nil
}

type denyGate struct{}

func (denyGate) Request(_ context.Context, _ string, _ []string, _ string) (bool, error) {
	return false, nil
}

type failGen struct{}

func (g failGen) Generate(_ context.Context, _ CoverageGap, _ []byte) (TestFile, error) {
	return TestFile{}, assert.AnError
}

func TestAutoTest_DryRun_NoWrite(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, allowGate{}, w, SafetyDryRun, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Empty(t, w.written)
	assert.Len(t, rep.Generated, 1)
}

func TestAutoTest_AutoMode_WritesAndRevertsOnNoGain(t *testing.T) {
	// stubProvider always returns 5/10 covered → after-write coverage == before → revert.
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"bad"}, allowGate{}, w, SafetyAuto, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Contains(t, rep.Reverted, "a_test.go")
	assert.Empty(t, w.written) // reverted → nothing left
}

func TestAutoTest_ApproveMode_GateDenied_Skips(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, denyGate{}, w, SafetyApprove, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Contains(t, rep.Skipped, "a_test.go")
	assert.Empty(t, w.written)
}

func TestAutoTest_AbortsOnFailingBaseline(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: false}, stubGen{"package p"}, allowGate{}, w, SafetyAuto, zap.NewNop())
	_, err := a.Run(context.Background(), ".")
	require.Error(t, err)
}

func TestAutoTest_Items_DryRun(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, allowGate{}, w, SafetyDryRun, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Len(t, rep.Items, 1, "should have 1 item for 1 gap")

	item := rep.Items[0]
	assert.Equal(t, "a.go", item.TargetFile, "target file should match gap")
	assert.Equal(t, "F", item.TargetFunc, "target func should match gap")
	assert.Equal(t, ReasonZeroCover, item.Reason, "reason should match gap")
	assert.Equal(t, "a_test.go", item.TestPath, "test path should match generated")
	assert.Equal(t, "generated", item.Status, "status should be generated in dry-run")
}

func TestAutoTest_Items_AutoMode_Reverted(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"bad"}, allowGate{}, w, SafetyAuto, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Len(t, rep.Items, 1, "should have 1 item for 1 gap")

	item := rep.Items[0]
	assert.Equal(t, "a.go", item.TargetFile)
	assert.Equal(t, "F", item.TargetFunc)
	assert.Equal(t, ReasonZeroCover, item.Reason)
	assert.Equal(t, "a_test.go", item.TestPath)
	assert.Equal(t, "reverted", item.Status, "status should be reverted when coverage doesn't improve")
}

func TestAutoTest_Items_ApproveMode_Skipped(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, stubGen{"package p"}, denyGate{}, w, SafetyApprove, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Len(t, rep.Items, 1, "should have 1 item for 1 gap")

	item := rep.Items[0]
	assert.Equal(t, "a.go", item.TargetFile)
	assert.Equal(t, "F", item.TargetFunc)
	assert.Equal(t, ReasonZeroCover, item.Reason)
	assert.Equal(t, "a_test.go", item.TestPath)
	assert.Equal(t, "skipped", item.Status, "status should be skipped when gate denies")
}

func TestAutoTest_Items_FailedGeneration(t *testing.T) {
	w := &memoryWriter{}
	a := NewAutoTest(stubProvider{pass: true}, failGen{}, allowGate{}, w, SafetyAuto, zap.NewNop())
	rep, err := a.Run(context.Background(), ".")
	// Should not error, but item should be marked failed
	require.NoError(t, err)
	assert.Len(t, rep.Items, 1)

	item := rep.Items[0]
	assert.Equal(t, "failed", item.Status, "status should be failed when generation fails")
	assert.Empty(t, item.TestPath, "test path should be empty on failure")
}

func TestAutoTest_ParallelExecution(t *testing.T) {
	w := &memoryWriter{}

	// Create a provider that returns multiple gaps
	multiGapProvider := &mockCoverageProvider{
		pass: true,
		gaps: []CoverageGap{
			{File: "a.go", Func: "F", Reason: ReasonZeroCover},
			{File: "b.go", Func: "G", Reason: ReasonZeroCover},
			{File: "c.go", Func: "H", Reason: ReasonZeroCover},
		},
	}

	a := NewAutoTest(multiGapProvider, stubGen{"package p"}, allowGate{}, w, SafetyDryRun, zap.NewNop())
	a.MaxConcurrency = 2 // Enable parallel execution
	a.MaxGaps = 0 // Disable gap limiting for this test

	rep, err := a.Run(context.Background(), ".")
	require.NoError(t, err)
	assert.Len(t, rep.Generated, 3, "should generate 3 tests for 3 gaps")
	assert.Len(t, rep.Items, 3, "should have 3 items for 3 gaps")
	assert.Empty(t, w.written, "dry-run should not write files")
}

// mockCoverageProvider is a configurable mock provider for testing
type mockCoverageProvider struct {
	pass bool
	gaps []CoverageGap
}

func (m *mockCoverageProvider) RunCoverage(_ context.Context, _ string) (*CoverageReport, error) {
	return &CoverageReport{Pass: m.pass, CoveredFuncs: 5, TotalFuncs: 10}, nil
}

func (m *mockCoverageProvider) Gaps(_ *CoverageReport) []CoverageGap {
	return m.gaps
}

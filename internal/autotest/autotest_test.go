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

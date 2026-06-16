package autotest

import (
	"context"
	"os"

	"go.uber.org/zap"
)

// RequestGate is the gate surface autotest needs: ask (or auto-approve) before a
// destructive write. escalation.Gate is adapted to this in Task 6.
type RequestGate interface {
	Request(ctx context.Context, checkpoint string, files []string, preview string) (bool, error)
}

// Writer writes a generated test and can revert it.
type Writer interface {
	Write(tf TestFile) error
	Revert(path string) error
}

// FSWriter is the default Writer: writes to disk, reverts via os.Remove.
type FSWriter struct{}

func (FSWriter) Write(tf TestFile) error  { return os.WriteFile(tf.Path, tf.Content, 0o644) }
func (FSWriter) Revert(path string) error { return os.Remove(path) }

// AutoTest coordinates automated test generation and coverage verification
type AutoTest struct {
	coverage       CoverageProvider
	gen            TestGenerator
	gate           RequestGate
	writer         Writer
	mode           SafetyMode
	MaxGaps        int  // cap on gaps generated per run (0 = unlimited); defaults to 5
	MaxConcurrency int  // max parallel workers (0 = serial); defaults to 3
	logger         *zap.Logger
}

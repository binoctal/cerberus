// Package logging builds cerberus's configured zap logger.
//
// It honors CERBERUS_LOG_LEVEL (resolved by the cmd/ entrypoints from
// config.Load) and tees JSON output to stderr AND a daily file under the
// runtime logs directory, so LLM-pipeline behavior is observable post-mortem
// without re-running.
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// NewLogger builds a zap logger that tees JSON output to stderr and a daily
// file under logsDir, at the parsed level. The daily file is named
// cerberus-YYYY-MM-DD.log (date fixed at process start) and opened append-only.
//
// A file-open failure degrades gracefully: the returned logger writes to
// stderr only and never aborts the run. Either way, one info line names where
// logs are written so the sink is discoverable.
//
// zap.AddCaller is applied to preserve the "caller" field that the previous
// zap.NewProduction() loggers emitted. Sampling is intentionally NOT applied
// so debug-level visibility is never dropped.
func NewLogger(level string, logsDir string) *zap.Logger {
	lvl := parseLevel(level)
	enc := zap.NewProductionEncoderConfig()
	stderrCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(enc),
		zapcore.Lock(os.Stderr),
		lvl,
	)
	fileCore, sink := newFileCore(logsDir, lvl, enc)
	if fileCore == nil {
		logger := zap.New(stderrCore, zap.AddCaller())
		logger.Info("logging to stderr only (file sink unavailable)", zap.String("dir", logsDir))
		return logger
	}
	logger := zap.New(zapcore.NewTee(stderrCore, fileCore), zap.AddCaller())
	logger.Info("logging to file", zap.String("path", sink))
	return logger
}

// dailyFile names the daily log file for the given instant.
func dailyFile(logsDir string, now time.Time) string {
	return filepath.Join(logsDir, "cerberus-"+now.Format("2006-01-02")+".log")
}

// newFileCore opens the daily file under logsDir and returns a JSON core at
// lvl writing to it (append). Returns (nil, "") if the directory/file cannot
// be created or opened, so the caller can degrade to stderr-only.
func newFileCore(logsDir string, lvl zapcore.Level, enc zapcore.EncoderConfig) (zapcore.Core, string) {
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, ""
	}
	path := dailyFile(logsDir, time.Now())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, ""
	}
	return zapcore.NewCore(
		zapcore.NewJSONEncoder(enc),
		zapcore.Lock(f),
		lvl,
	), path
}

// parseLevel maps a level string to zapcore.Level. Unknown values (including
// the empty string) fall back to InfoLevel, matching zap.NewProduction().
func parseLevel(level string) zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

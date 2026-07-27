package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want zapcore.Level
	}{
		{"debug", "debug", zapcore.DebugLevel},
		{"info", "info", zapcore.InfoLevel},
		{"warn", "warn", zapcore.WarnLevel},
		{"warning-alias", "warning", zapcore.WarnLevel},
		{"error", "error", zapcore.ErrorLevel},
		{"empty", "", zapcore.InfoLevel},
		{"unknown", "verbose", zapcore.InfoLevel},
		{"uppercase", "DEBUG", zapcore.DebugLevel},
		{"padded", "  info  ", zapcore.InfoLevel},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseLevel(c.in); got != c.want {
				t.Errorf("parseLevel(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestDailyFile(t *testing.T) {
	got := dailyFile("/tmp/logs", time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC))
	want := filepath.Join("/tmp/logs", "cerberus-2026-07-27.log")
	if got != want {
		t.Errorf("dailyFile = %q, want %q", got, want)
	}
}

func TestNewLogger_WritesDailyFile(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger("debug", dir)
	logger.Debug("hello-from-test")
	_ = logger.Sync()

	name := "cerberus-" + time.Now().Format("2006-01-02") + ".log"
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("expected daily file %s: %v", name, err)
	}
	if !strings.Contains(string(data), "hello-from-test") {
		t.Errorf("daily file missing debug line; got:\n%s", data)
	}
}

func TestNewLogger_LevelFilters(t *testing.T) {
	dir := t.TempDir()
	logger := NewLogger("info", dir)
	logger.Debug("should-be-filtered")
	logger.Info("should-pass")
	_ = logger.Sync()

	name := "cerberus-" + time.Now().Format("2006-01-02") + ".log"
	data, _ := os.ReadFile(filepath.Join(dir, name))
	if strings.Contains(string(data), "should-be-filtered") {
		t.Error("debug line leaked into info-level file")
	}
	if !strings.Contains(string(data), "should-pass") {
		t.Error("info line missing from file")
	}
}

func TestNewLogger_FileOpenFails_Graceful(t *testing.T) {
	// logsDir points at an existing FILE, so MkdirAll fails -> stderr fallback.
	filePath := filepath.Join(t.TempDir(), "i-am-a-file")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	logger := NewLogger("debug", filePath) // must not panic
	logger.Debug("still-works")
	_ = logger.Sync()
}

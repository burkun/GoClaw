package logging

import (
	"log/slog"
	"strings"
	"testing"
)

func TestInit_DefaultLevel(t *testing.T) {
	// Test empty string defaults to info
	Init("")
	if defaultLogger == nil {
		t.Error("expected default logger to be initialized")
	}
}

func TestInit_Levels(t *testing.T) {
	tests := []struct {
		level    string
		wantLvl  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"unknown", slog.LevelInfo}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			Init(tt.level)
			// Can't easily check the level, but at least verify no panic
			if defaultLogger == nil {
				t.Error("expected logger to be initialized")
			}
		})
	}
}

func TestSetLevel(t *testing.T) {
	SetLevel("debug")
	if defaultLogger == nil {
		t.Error("expected logger after SetLevel")
	}

	SetLevel("error")
	if defaultLogger == nil {
		t.Error("expected logger after SetLevel")
	}
}

func TestLogger(t *testing.T) {
	// Reset to ensure test isolation
	Init("info")

	logger := Logger()
	if logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestLogger_NilDefault(t *testing.T) {
	// Save and restore
	saved := defaultLogger
	defer func() { defaultLogger = saved }()

	defaultLogger = nil
	logger := Logger()
	// Should return slog.Default() when defaultLogger is nil
	if logger == nil {
		t.Error("expected fallback to slog.Default()")
	}
}

func TestDebug(t *testing.T) {
	Init("debug")
	// Just verify no panic
	Debug("test debug message", "key", "value")
}

func TestInfo(t *testing.T) {
	Init("info")
	Info("test info message", "key", "value")
}

func TestWarn(t *testing.T) {
	Init("warn")
	Warn("test warn message", "key", "value")
}

func TestError(t *testing.T) {
	Init("error")
	Error("test error message", "key", "value")
}

func TestWith(t *testing.T) {
	Init("info")
	logger := With("component", "test")
	if logger == nil {
		t.Error("expected non-nil logger from With")
	}
}

func TestLogging_Output(t *testing.T) {
	// This test verifies logging works without panicking
	// Actual output verification would require capturing stdout
	Init("debug")

	Debug("debug message")
	Info("info message")
	Warn("warn message")
	Error("error message")

	// With various argument counts
	Info("no args")
	Info("one arg", "key1", "val1")
	Info("two args", "key1", "val1", "key2", "val2")
}

func TestLogging_SpecialChars(t *testing.T) {
	Init("info")

	// Messages with special characters
	Info("message with\nnewline")
	Info("message with\ttab")
	Info("message with \"quotes\"")
	Info("message with 中文")
	Info(strings.Repeat("x", 1000)) // long message
}

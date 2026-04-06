// Package logging provides unified logging for goclaw.
// It uses log/slog (Go 1.21+ standard library) for structured logging.
package logging

import (
	"log/slog"
	"os"
)

var defaultLogger *slog.Logger

// Init initializes the default logger with the given level.
// Supported levels: debug, info, warn, error (case-insensitive).
// Falls back to info level for unrecognized values.
func Init(level string) {
	var lvl slog.Level
	switch level {
	case "debug", "DEBUG":
		lvl = slog.LevelDebug
	case "info", "INFO", "":
		lvl = slog.LevelInfo
	case "warn", "WARN", "warning", "WARNING":
		lvl = slog.LevelWarn
	case "error", "ERROR":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	defaultLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: lvl,
	}))
	slog.SetDefault(defaultLogger)
}

// SetLevel changes the log level at runtime.
func SetLevel(level string) {
	Init(level)
}

// Logger returns the default slog.Logger.
func Logger() *slog.Logger {
	if defaultLogger == nil {
		return slog.Default()
	}
	return defaultLogger
}

// Debug logs a message at Debug level.
func Debug(msg string, args ...any) {
	Logger().Debug(msg, args...)
}

// Info logs a message at Info level.
func Info(msg string, args ...any) {
	Logger().Info(msg, args...)
}

// Warn logs a message at Warn level.
func Warn(msg string, args ...any) {
	Logger().Warn(msg, args...)
}

// Error logs a message at Error level.
func Error(msg string, args ...any) {
	Logger().Error(msg, args...)
}

// With returns a logger with additional context.
func With(args ...any) *slog.Logger {
	return Logger().With(args...)
}

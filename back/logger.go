package main

import (
	"log/slog"
	"os"
	"strings"
)

var appLogger = newLogger()

func init() {
	slog.SetDefault(appLogger)
}

func newLogger() *slog.Logger {
	level := &slog.LevelVar{}
	level.Set(parseLogLevel(os.Getenv("LOG_LEVEL")))

	opts := &slog.HandlerOptions{Level: level}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("LOG_FORMAT"))) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	default:
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
}

func parseLogLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func logDebug(component, msg string, args ...any) {
	appLogger.Debug(msg, withComponent(component, args)...)
}

func logInfo(component, msg string, args ...any) {
	appLogger.Info(msg, withComponent(component, args)...)
}

func logWarn(component, msg string, args ...any) {
	appLogger.Warn(msg, withComponent(component, args)...)
}

func logError(component, msg string, args ...any) {
	appLogger.Error(msg, withComponent(component, args)...)
}

func withComponent(component string, args []any) []any {
	fields := make([]any, 0, len(args)+2)
	fields = append(fields, "component", component)
	fields = append(fields, args...)
	return fields
}

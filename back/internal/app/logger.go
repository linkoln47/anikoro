package app

import (
	"log/slog"
	"os"
	"strings"
	"time"
)

const logTimeFormat = "06:01:02T15:04:05"

func newLogger(cfg AppConfig) *slog.Logger {
	level := &slog.LevelVar{}
	level.Set(parseLogLevel(cfg.LogLevel))

	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, attr slog.Attr) slog.Attr {
			if attr.Key == slog.TimeKey {
				if ts, ok := attr.Value.Any().(time.Time); ok {
					attr.Value = slog.StringValue(ts.Format(logTimeFormat))
				}
			}
			return attr
		},
	}
	switch strings.ToLower(strings.TrimSpace(cfg.LogFormat)) {
	case "json":
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	default:
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
}

func (a *App) logDebug(component, msg string, args ...any) {
	a.Logger.Debug(msg, withComponent(component, args)...)
}

func (a *App) logInfo(component, msg string, args ...any) {
	a.Logger.Info(msg, withComponent(component, args)...)
}

func (a *App) logWarn(component, msg string, args ...any) {
	a.Logger.Warn(msg, withComponent(component, args)...)
}

func (a *App) logError(component, msg string, args ...any) {
	a.Logger.Error(msg, withComponent(component, args)...)
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

func withComponent(component string, args []any) []any {
	fields := make([]any, 0, len(args)+2)
	fields = append(fields, "component", component)
	fields = append(fields, args...)
	return fields
}

type appSyncLogger struct {
	app *App
}

func (logger appSyncLogger) Debug(component, msg string, args ...any) {
	logger.app.logDebug(component, msg, args...)
}

func (logger appSyncLogger) Info(component, msg string, args ...any) {
	logger.app.logInfo(component, msg, args...)
}

func (logger appSyncLogger) Warn(component, msg string, args ...any) {
	logger.app.logWarn(component, msg, args...)
}

func (logger appSyncLogger) Error(component, msg string, args ...any) {
	logger.app.logError(component, msg, args...)
}

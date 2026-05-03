package config

import (
	"log/slog"
	"os"
	"strings"
)

var Logger = newLogger()

func newLogger() *slog.Logger {
	return newLoggerWithLevel(os.Getenv("LOG_LEVEL"))
}

func newLoggerWithLevel(raw string) *slog.Logger {
	level := new(slog.LevelVar)
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "DEBUG":
		level.Set(slog.LevelDebug)
	case "WARN":
		level.Set(slog.LevelWarn)
	case "ERROR":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}

func RefreshLogger() {
	Logger = newLogger()
}

func RefreshLoggerWithLevel(raw string) {
	Logger = newLoggerWithLevel(raw)
}

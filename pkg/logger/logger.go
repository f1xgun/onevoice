package logger

import (
	"log/slog"
	"os"
	"strings"
)

// Config holds structured logger configuration.
type Config struct {
	Service string
	Env     string     // from ENV env var, default "development"
	Version string     // from VERSION env var, default "dev"
	Level   slog.Level // from LOG_LEVEL env var, default LevelInfo
}

// NewFromConfig creates a structured JSON logger from explicit configuration.
func NewFromConfig(cfg Config) *slog.Logger {
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.Level,
	})
	ctxHandler := NewContextHandler(jsonHandler)
	return slog.New(ctxHandler).With(
		slog.String("service", cfg.Service),
		slog.String("env", cfg.Env),
		slog.String("version", cfg.Version),
	)
}

// New creates a structured JSON logger for the given service.
// It reads ENV, VERSION, and LOG_LEVEL environment variables.
func New(service string) *slog.Logger {
	return NewFromConfig(Config{
		Service: service,
		Env:     envOrDefault("ENV", "development"),
		Version: envOrDefault("VERSION", "dev"),
		Level:   parseLogLevel(os.Getenv("LOG_LEVEL")),
	})
}

// NewWithLevel creates a structured JSON logger with a specific log level.
// It reads ENV and VERSION environment variables.
func NewWithLevel(service string, level slog.Level) *slog.Logger {
	return NewFromConfig(Config{
		Service: service,
		Env:     envOrDefault("ENV", "development"),
		Version: envOrDefault("VERSION", "dev"),
		Level:   level,
	})
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

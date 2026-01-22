package shim

import (
	"log/slog"
	"os"
	"strings"
)

type LogMode string

const (
	LogJSON LogMode = "json"
	LogText LogMode = "text"
)

type LogConfig struct {
	Mode  LogMode
	Level slog.Level
	// Component ajuda a diferenciar "shim-proc" vs "shim-xport"
	Component string
}

// NewLogger cria um logger slog que SEMPRE escreve em stderr.
// Nunca use stdout no shim.
func NewLogger(cfg LogConfig) *slog.Logger {
	if cfg.Component == "" {
		cfg.Component = "shim"
	}

	opts := &slog.HandlerOptions{Level: cfg.Level}

	var h slog.Handler
	switch cfg.Mode {
	case LogText:
		h = slog.NewTextHandler(os.Stderr, opts)
	default:
		h = slog.NewJSONHandler(os.Stderr, opts)
	}

	l := slog.New(h).With(
		slog.String("component", cfg.Component),
	)

	return l
}

// ParseLogModeFromEnv lê SHIM_LOG_MODE={json|text}. Default: json
func ParseLogModeFromEnv() LogMode {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SHIM_LOG_MODE")))
	switch v {
	case "text":
		return LogText
	case "json", "":
		return LogJSON
	default:
		return LogJSON
	}
}

// ParseLogLevelFromEnv lê SHIM_LOG_LEVEL={debug|info|warn|error}. Default: info
func ParseLogLevelFromEnv() slog.Level {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SHIM_LOG_LEVEL")))
	switch v {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	case "info", "":
		return slog.LevelInfo
	default:
		return slog.LevelInfo
	}
}

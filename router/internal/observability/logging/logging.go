package logging

import (
	"log/slog"
	"os"
)

type Mode string

const (
	ModeJSON Mode = "json"
	ModeText Mode = "text"
)

type Config struct {
	Mode  Mode
	Level slog.Level
}

func New(cfg Config) *slog.Logger {
	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level: cfg.Level,
	}

	switch cfg.Mode {
	case ModeText:
		handler = slog.NewTextHandler(os.Stderr, opts)
	default:
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return logger
}

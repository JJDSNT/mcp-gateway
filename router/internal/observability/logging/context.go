package logging

import (
	"context"
	"log/slog"
	"strings"

	"github.com/google/uuid"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	loggerKey
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// EnsureRequestID mantém compatibilidade: gera request_id se não existir no ctx.
func EnsureRequestID(ctx context.Context) (context.Context, string) {
	if id := RequestIDFromContext(ctx); id != "" {
		return ctx, id
	}
	id := uuid.NewString()
	return WithRequestID(ctx, id), id
}

// EnsureRequestIDWithIncoming usa um request_id "de fora" (ex: header X-Request-Id) se vier.
// Regras:
// - se incoming não vazio, ele vence e é gravado no ctx
// - senão, mantém o do ctx se existir
// - senão, gera um novo
func EnsureRequestIDWithIncoming(ctx context.Context, incoming string) (context.Context, string) {
	incoming = strings.TrimSpace(incoming)
	if incoming != "" {
		return WithRequestID(ctx, incoming), incoming
	}
	return EnsureRequestID(ctx)
}

func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

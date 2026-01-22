package logging

import (
	"log/slog"
	"net/http"
	"time"
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		incomingRID := r.Header.Get("X-Request-Id")
		ctx, rid := EnsureRequestID(r.Context(), incomingRID)

		// propaga para o cliente
		w.Header().Set("X-Request-Id", rid)

		logger := slog.Default().With(
			RequestID(rid),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("remote_ip", r.RemoteAddr),
		)

		ctx = WithLogger(ctx, logger)
		r = r.WithContext(ctx)

		next.ServeHTTP(w, r)

		logger.Info("request completed",
			DurationMs(time.Since(start).Milliseconds()),
		)
	})
}

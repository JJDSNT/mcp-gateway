package logging

import (
	"context"
	"net/http"
)

// Middleware injeta request_id e logger no context da request.
// - request_id vem do header X-Request-Id (se existir) ou é gerado.
// - logger é slog.Default() (ou o que você setou com logging.New()) com request_id.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Se o cliente mandou um request id, aproveita; senão gera.
		// (Header canonical: X-Request-Id / X-Request-ID. Vamos aceitar ambas.)
		hid := r.Header.Get("X-Request-Id")
		if hid == "" {
			hid = r.Header.Get("X-Request-ID")
		}

		ctx := r.Context()

		// ✅ API atual: EnsureRequestID(ctx) (não recebe id)
		// Então: se veio header, usa WithRequestID; senão, gera com EnsureRequestID.
		var rid string
		if hid != "" {
			rid = hid
			ctx = WithRequestID(ctx, rid)
		} else {
			var newCtx context.Context
			newCtx, rid = EnsureRequestID(ctx)
			ctx = newCtx
		}

		// Injeta logger request-scoped (sem tool/runtime aqui; esses entram nos handlers)
		log := LoggerFromContext(ctx).With(RequestID(rid))
		ctx = WithLogger(ctx, log)

		// (Opcional) ecoar request id de volta ajuda debug com proxies/tunnel
		w.Header().Set("X-Request-Id", rid)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

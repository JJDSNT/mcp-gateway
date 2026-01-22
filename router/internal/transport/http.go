package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"mcp-router/internal/core"
	"mcp-router/internal/observability/logging"
	"mcp-router/internal/runtime"
	"mcp-router/internal/sandbox"
)

const maxRequestBodyBytes = 1 << 20 // 1MB

type HTTP struct {
	core *core.Service
}

func NewHTTP(c *core.Service) *HTTP {
	return &HTTP{core: c}
}

// Register registra as rotas HTTP do gateway.
func (h *HTTP) Register(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.handleHealthz)
	mux.HandleFunc("/readyz", h.handleReadyz)

	mux.HandleFunc("/mcp/tools", h.handleTools)
	mux.HandleFunc("/mcp/", h.handleMCP)
}

// Run sobe o servidor HTTP e faz shutdown gracioso quando ctx for cancelado.
//
// Importante: o handler do server é embrulhado com hardening (bloqueia dot-segments antes do ServeMux).
func (h *HTTP) Run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           WrapHardening(logging.Middleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,                // SSE
		IdleTimeout:       60 * time.Second, // keep-alive
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// WrapHardening bloqueia paths com dot-segments antes do ServeMux tentar limpar e redirecionar.
// Isso garante 400 (e não 301) para tentativas como /mcp/../evil.
func WrapHardening(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Só aplica no namespace /mcp
		if !strings.HasPrefix(r.URL.Path, "/mcp") {
			next.ServeHTTP(w, r)
			return
		}

		// Bloqueia dot-segments no path "decodificado"
		p := r.URL.Path
		if strings.Contains(p, "/../") || strings.HasSuffix(p, "/..") ||
			strings.Contains(p, "/./") || strings.HasSuffix(p, "/.") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// Bloqueia também tentativas URL-encoded (escaped path)
		ep := strings.ToLower(r.URL.EscapedPath())
		// cobre %2e%2e%2f, %2e%2e/, %2e/ etc
		if strings.Contains(ep, "%2e%2e") || strings.Contains(ep, "%2e/") || strings.Contains(ep, "/%2e") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *HTTP) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (h *HTTP) handleReadyz(w http.ResponseWriter, r *http.Request) {
	tools, err := h.core.ListTools(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ready":  false,
			"reason": "list_tools_failed",
			"error":  err.Error(),
		})
		return
	}

	needsDocker := false
	for _, t := range tools {
		if t.Runtime == "container" {
			needsDocker = true
			break
		}
	}

	runtimes := map[string]any{
		"native": true,
	}

	if needsDocker {
		if err := runtime.DockerReady(r.Context()); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ready":  false,
				"reason": "docker_unavailable",
				"error":  err.Error(),
				"runtimes": map[string]any{
					"native":    true,
					"container": false,
				},
			})
			return
		}
		runtimes["container"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ready":         true,
		"config_loaded": true,
		"tools":         len(tools),
		"runtimes":      runtimes,
	})
}

func (h *HTTP) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	tools, err := h.core.ListTools(r.Context())
	if err != nil {
		http.Error(w, "failed to list tools", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tools": tools})
}

func (h *HTTP) handleMCP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Content-Type precisa ser application/json
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
	toolName = strings.Trim(toolName, "/")

	if err := sandbox.ValidateToolName(toolName); err != nil {
		http.Error(w, "invalid tool name", http.StatusBadRequest)
		return
	}

	// body bounded
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		body = []byte(`{}`)
	}
	if !json.Valid(body) {
		http.Error(w, "body must be valid JSON", http.StatusBadRequest)
		return
	}

	// runtime (best effort via ListTools) - usado só para header/log
	rt := h.lookupRuntime(r.Context(), toolName)

	// request-scoped logger (from middleware) + fixed fields
	rid := logging.RequestIDFromContext(r.Context())
	logger := logging.LoggerFromContext(r.Context()).With(
		logging.Tool(toolName),
		logging.Runtime(rt),
		logging.RequestID(rid),
	)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		logger.Error("streaming unsupported",
			logging.Err(fmt.Errorf("http.Flusher not supported")),
			logging.DurationMs(time.Since(start).Milliseconds()),
		)
		return
	}

	// SSE headers (somente depois de validar tudo)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-MCP-Tool", toolName)

	// timeout (best effort via core helper)
	if d, ok := h.core.ToolTimeout(toolName); ok {
		w.Header().Set("X-MCP-Timeout", d.String())
	}
	if rt != "" {
		w.Header().Set("X-MCP-Runtime", rt)
	}

	state := &streamState{}
	sse := &sseWriter{w: w, f: flusher, state: state}

	// r.Context() é cancelado quando o cliente desconecta.
	err = h.core.StreamTool(r.Context(), toolName, body, sse)
	if err != nil {
		// regra: erro antes do primeiro evento -> HTTP error
		if state.canHTTPError() {
			// mapeia concorrência para 429 (fail-fast)
			if errors.Is(err, core.ErrToolBusy) {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "tool busy", http.StatusTooManyRequests)
				logger.Warn("tool busy (concurrency limit)",
					logging.Err(err),
					logging.DurationMs(time.Since(start).Milliseconds()),
				)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			logger.Error("tool stream failed before first event",
				logging.Err(err),
				logging.DurationMs(time.Since(start).Milliseconds()),
			)
			return
		}

		// regra: erro após início do streaming -> log + (opcional) event:error único
		logger.Error("tool stream failed after start",
			logging.Err(err),
			logging.DurationMs(time.Since(start).Milliseconds()),
		)

		// Evita múltiplos erros em SSE
		state.trySendStreamError(func() error {
			// Para busy pós-início (raro), também vira error event.
			msg := err.Error()
			if errors.Is(err, core.ErrToolBusy) {
				msg = "tool busy"
			}
			return sendSSE(w, "error", map[string]string{"error": msg})
		})
		flusher.Flush()
		return
	}

	logger.Info("tool stream completed",
		logging.DurationMs(time.Since(start).Milliseconds()),
	)
}

// lookupRuntime pega runtime via ListTools (para header). Evita o transport conhecer config diretamente.
func (h *HTTP) lookupRuntime(ctx context.Context, toolName string) string {
	tools, err := h.core.ListTools(ctx)
	if err != nil {
		return ""
	}
	for _, t := range tools {
		if t.Name == toolName {
			return t.Runtime
		}
	}
	return ""
}

// streamState controla a semântica de erro SSE:
// - erro antes de iniciar streaming -> HTTP error
// - erro após iniciar streaming -> log + event:error (opcional)
// - evita múltiplos eventos de erro
type streamState struct {
	started   bool
	errorSent bool
}

func (s *streamState) markStarted() { s.started = true }
func (s *streamState) canHTTPError() bool {
	return !s.started
}
func (s *streamState) trySendStreamError(send func() error) {
	if s.errorSent {
		return
	}
	s.errorSent = true
	_ = send()
}

// sseWriter implementa core.LineWriter.
type sseWriter struct {
	w     http.ResponseWriter
	f     http.Flusher
	state *streamState
}

func (s *sseWriter) WriteLine(line []byte) error {
	if !s.state.started {
		s.state.markStarted()
	}
	if err := sendRawSSE(s.w, "message", line); err != nil {
		return err
	}
	s.f.Flush()
	return nil
}

func sendSSE(w http.ResponseWriter, event string, payload any) error {
	data, _ := json.Marshal(payload)
	return sendRawSSE(w, event, data)
}

func sendRawSSE(w http.ResponseWriter, event string, data []byte) error {
	if _, err := fmt.Fprintf(w, "event: %s\n", event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", bytes.TrimSpace(data)); err != nil {
		return err
	}
	return nil
}

package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"mcp-router/internal/core"
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
	mux.HandleFunc("/mcp/tools", h.handleTools)
	mux.HandleFunc("/mcp/", h.handleMCP)
}

// Run sobe o servidor HTTP e faz shutdown gracioso quando ctx for cancelado.
func (h *HTTP) Run(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	h.Register(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
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

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-MCP-Tool", toolName)

	// timeout (best effort via core helper)
	if d, ok := h.core.ToolTimeout(toolName); ok {
		w.Header().Set("X-MCP-Timeout", d.String())
	}

	// runtime (best effort via ListTools)
	if rt := h.lookupRuntime(r.Context(), toolName); rt != "" {
		w.Header().Set("X-MCP-Runtime", rt)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Cada linha de stdout vira um SSE event "message"
	sse := &sseWriter{w: w, f: flusher}

	// r.Context() é cancelado quando o cliente desconecta.
	if err := h.core.StreamTool(r.Context(), toolName, body, sse); err != nil {
		_ = sendSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
		return
	}
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

// sseWriter implementa core.LineWriter.
type sseWriter struct {
	w http.ResponseWriter
	f http.Flusher
}

func (s *sseWriter) WriteLine(line []byte) error {
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

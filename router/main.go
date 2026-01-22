package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/runner"
	"mcp-router/internal/sandbox"
)

var cfg *config.Config
var run *runner.Runner

const maxRequestBodyBytes = 1 << 20 // 1MB

func main() {
	var err error
	cfg, err = config.LoadFromFile("/config/config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	run = runner.New(cfg)

	log.Println("Loaded tools:")
	for k := range cfg.Tools {
		log.Println(" -", k)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/", handleMCP)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0,                 // SSE
		IdleTimeout:       60 * time.Second, // keep-alive
	}

	// Shutdown gracioso
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Println("MCP Router listening on :8080")
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	} else {
		log.Println("http server stopped")
	}
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
		// ok
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == http.MethodPost {
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
	}

	toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
	toolName = strings.Trim(toolName, "/")

	if err := sandbox.ValidateToolName(toolName); err != nil {
		log.Printf("invalid tool name %q: %v", toolName, err)
		http.Error(w, "invalid tool name", http.StatusBadRequest)
		return
	}

	tool, ok := cfg.Tools[toolName]
	if !ok {
		http.Error(w, "unknown tool", http.StatusNotFound)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	body = bytes.TrimSpace(body)
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
	w.Header().Set("X-MCP-Runtime", tool.Runtime)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// O r.Context() é automaticamente cancelado quando o cliente TCP desconecta.
	ctx, cancel := context.WithTimeout(r.Context(), tool.Timeout())
	defer cancel()

	log.Printf("request tool=%s runtime=%s timeout=%s", toolName, tool.Runtime, tool.Timeout())

	err = runLauncher(ctx, toolName, tool, body, w, flusher)
	if err != nil {
		log.Printf("[tool=%s] launcher error: %v", toolName, err)
		// Envio de erro só funciona se o cliente ainda estiver conectado
		_ = sendSSE(w, "error", map[string]string{"error": err.Error()})
		flusher.Flush()
	}
}

func runLauncher(
	ctx context.Context,
	toolName string,
	tool config.Tool,
	payload []byte,
	w http.ResponseWriter,
	flusher http.Flusher,
) error {
	proc, err := run.Start(ctx, toolName, tool)
	if err != nil {
		return err
	}

	// Monitor de Contexto: Força o proc.Close() (que por sua vez chama o KillProcess e fecha os pipes)
	// assim que o cliente desconecta. Isso desbloqueia o scanner.Scan() na goroutine abaixo.
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			proc.Close()
		case <-done:
		}
	}()
	defer close(done)
	defer proc.Close()

	stdin := proc.Stdin()
	_, err = stdin.Write(append(payload, '\n'))
	if err != nil {
		return err
	}
	stdin.Close()

	stdout := proc.Stdout()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	lines := make(chan []byte, 8)
	scanErr := make(chan error, 1)

	go func() {
		defer close(lines)
		for scanner.Scan() {
			b := append([]byte(nil), scanner.Bytes()...)
			lines <- b
		}
		scanErr <- scanner.Err()
	}()

	for {
		select {
		case <-ctx.Done():
			// O processo será morto pelo monitor acima ou pelo defer proc.Close()
			return ctx.Err()

		case line, ok := <-lines:
			if !ok {
				if err := <-scanErr; err != nil {
					return err
				}
				return proc.Wait()
			}

			if err := sendRawSSE(w, "message", line); err != nil {
				// Falha no envio (ex: Broken Pipe) indica desconexão do cliente.
				return err
			}
			flusher.Flush()
		}
	}
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
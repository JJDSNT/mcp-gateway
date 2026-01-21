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
		WriteTimeout:      0,                // SSE
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
		// se o server cair por outro motivo
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
	
	// P1: hardening de métodos
    switch r.Method {
    case http.MethodGet, http.MethodPost:
        // ok
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // P1: validar Content-Type quando aplicável (POST com JSON)
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

	// Validar tool name (P0: bloqueia caracteres suspeitos)
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

	// timeout por tool
	ctx, cancel := context.WithTimeout(r.Context(), tool.Timeout())
	defer cancel()

	log.Printf("request tool=%s runtime=%s timeout=%s", toolName, tool.Runtime, tool.Timeout())

	err = runLauncher(ctx, toolName, tool, body, w, flusher)
	if err != nil {
		sendSSE(w, "error", map[string]string{"error": err.Error()})
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
	defer proc.Close()

	stdin := proc.Stdin()
	_, err = stdin.Write(append(payload, '\n'))
	if err != nil {
		return err
	}

	if err := stdin.Close(); err != nil {
		// não falha request por isso; só loga
		log.Printf("[tool=%s] stdin close error: %v", toolName, err)
	}

	stdout := proc.Stdout()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	// Canaliza linhas e erros para conseguir interromper imediatamente no ctx.Done().
	lines := make(chan []byte, 8)
	scanErr := make(chan error, 1)

	go func() {
		defer close(lines)

		for scanner.Scan() {
			// Copia o buffer do scanner, porque scanner.Bytes() muda a cada Scan()
			b := append([]byte(nil), scanner.Bytes()...)
			lines <- b
		}
		// scanner terminou: registra erro (ou nil)
		scanErr <- scanner.Err()
	}()

	for {
		select {
		case <-ctx.Done():
			// Cliente desconectou / timeout da tool: mata o processo via defer proc.Close()
			return ctx.Err()

		case line, ok := <-lines:
			if !ok {
				// Acabou o scan: pega erro do scanner e depois espera o processo
				if err := <-scanErr; err != nil {
					return err
				}
				return proc.Wait()
			}

			sendRawSSE(w, "message", line)
			flusher.Flush()
		}
	}
}


func sendSSE(w http.ResponseWriter, event string, payload any) {
	data, _ := json.Marshal(payload)
	sendRawSSE(w, event, data)
}

func sendRawSSE(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", bytes.TrimSpace(data))
}

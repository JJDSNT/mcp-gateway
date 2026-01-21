package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"router/internal/config"
	"router/internal/runner"
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
	toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
	toolName = strings.Trim(toolName, "/")

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

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		sendRawSSE(w, "message", line)
		flusher.Flush()
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return proc.Wait()
}

func sendSSE(w http.ResponseWriter, event string, payload any) {
	data, _ := json.Marshal(payload)
	sendRawSSE(w, event, data)
}

func sendRawSSE(w http.ResponseWriter, event string, data []byte) {
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", bytes.TrimSpace(data))
}

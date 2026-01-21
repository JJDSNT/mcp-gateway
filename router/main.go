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
	"strings"

	"router/internal/config"
	"router/internal/runner"
)

var cfg *config.Config
var run *runner.Runner

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

	http.HandleFunc("/mcp/", handleMCP)

	log.Println("MCP Router listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
	toolName = strings.Trim(toolName, "/")

	tool, ok := cfg.Tools[toolName]
	if !ok {
		http.Error(w, "unknown tool", http.StatusNotFound)
		return
	}

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

	ctx := r.Context()

	log.Printf("request tool=%s runtime=%s", toolName, tool.Runtime)

	err = runLauncher(ctx, tool, body, w, flusher)
	if err != nil {
		sendSSE(w, "error", map[string]string{
			"error": err.Error(),
		})
		flusher.Flush()
	}
}

func runLauncher(
	ctx context.Context,
	tool config.Tool,
	payload []byte,
	w http.ResponseWriter,
	flusher http.Flusher,
) error {
	proc, err := run.Start(ctx, tool)
	if err != nil {
		return err
	}
	defer proc.Close()

	// send request
	stdin := proc.Stdin()
	_, err = stdin.Write(append(payload, '\n'))
	if err != nil {
		return err
	}
	stdin.Close()

	// stream stdout to SSE
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

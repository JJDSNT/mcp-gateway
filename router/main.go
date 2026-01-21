// router/main.go
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
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

type Tool struct {
	Runtime string   `yaml:"runtime"` // native | container
	Mode    string   `yaml:"mode"`    // launcher | daemon (daemon reservado)
	Cmd     string   `yaml:"cmd"`
	Image   string   `yaml:"image"`
	Args    []string `yaml:"args"`
}

type Config struct {
	WorkspaceRoot string          `yaml:"workspace_root"`
	ToolsRoot     string          `yaml:"tools_root"`
	Tools         map[string]Tool `yaml:"tools"`
}

var cfg Config

func main() {
	loadConfig()

	http.HandleFunc("/mcp/", handleMCP)

	log.Println("MCP Router listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadConfig() {
	data, err := os.ReadFile("/config/config.yaml")
	if err != nil {
		log.Fatal("cannot read config.yaml:", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		log.Fatal("invalid config.yaml:", err)
	}

	log.Println("Loaded tools:")
	for k := range cfg.Tools {
		log.Println(" -", k)
	}
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	toolName := strings.TrimPrefix(r.URL.Path, "/mcp/")
	toolName = strings.Trim(toolName, "/")

	tool, ok := cfg.Tools[toolName]
	if !ok {
		http.Error(w, "unknown tool", 404)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid body", 400)
		return
	}

	body = bytes.TrimSpace(body)

	if !json.Valid(body) {
		http.Error(w, "body must be valid JSON", 400)
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
		http.Error(w, "streaming unsupported", 500)
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

func runLauncher(ctx context.Context, tool Tool, payload []byte, w http.ResponseWriter, flusher http.Flusher) error {
	cmd, stdin, stdout, err := spawnTool(ctx, tool)
	if err != nil {
		return err
	}

	defer killProcess(cmd)

	// send request
	_, err = stdin.Write(append(payload, '\n'))
	if err != nil {
		return err
	}

	stdin.Close()

	// stream stdout to SSE
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

	return cmd.Wait()
}

func spawnTool(ctx context.Context, tool Tool) (*exec.Cmd, io.WriteCloser, io.ReadCloser, error) {

	env := append(os.Environ(),
		"WORKSPACE_ROOT="+cfg.WorkspaceRoot,
		"TOOLS_ROOT="+cfg.ToolsRoot,
	)

	var cmd *exec.Cmd

	// Native Runtime
	if tool.Runtime == "native" {
		cmd = exec.CommandContext(ctx, tool.Cmd, tool.Args...)
		cmd.Env = env
	}

	// Container Runtime
	if tool.Runtime == "container" {
		args := []string{
			"run", "-i", "--rm",
			"-v", fmt.Sprintf("%s:/workspaces", cfg.WorkspaceRoot),
			tool.Image,
		}
		args = append(args, tool.Args...)

		cmd = exec.CommandContext(ctx, "docker", args...)
		cmd.Env = env
	}

	if cmd == nil {
		return nil, nil, nil, fmt.Errorf("invalid runtime: %s", tool.Runtime)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, err
	}

	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, err
	}

	// log stderr
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Printf("[tool stderr] %s", sc.Text())
		}
	}()

	return cmd, stdin, stdout, nil
}

func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Kill()
		cmd.Wait()
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

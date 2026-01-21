package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/runner"
)

func setTestConfigAndRunnerForDisconnect(t *testing.T, markerFile string) {
	t.Helper()

	cfg = &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			"echo": {
				Runtime: "native",
				Cmd:     os.Args[0],
				Args:    []string{"__mcp_tool_disconnect_helper__"},
			},
		},
	}
	run = runner.New(cfg)

	// Faz o TestMain entrar no modo helper quando o subprocesso rodar.
	t.Setenv("MCP_ROUTER_TEST_TOOL", "1")
	// Marker file para o subprocesso escrever no defer ao encerrar.
	t.Setenv("MCP_TOOL_EXIT_MARKER", markerFile)
}

func TestSSEDisconnect_KillsToolProcess(t *testing.T) {
	marker := t.TempDir() + "/tool_exited.marker"
	setTestConfigAndRunnerForDisconnect(t, marker)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(handleMCP))
	srv.EnableHTTP2 = false
	srv.Start()
	defer srv.Close()

	// Abre uma conexão TCP crua para conseguir "dropar" no meio do streaming.
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	reqBody := `{}`

	rawReq := "" +
		"POST /mcp/echo HTTP/1.1\r\n" +
		"Host: test\r\n" +
		"Content-Type: application/json\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(reqBody)) +
		"\r\n" +
		reqBody

	if _, err := conn.Write([]byte(rawReq)); err != nil {
		_ = conn.Close()
		t.Fatalf("write request: %v", err)
	}

	br := bufio.NewReader(conn)

	// Status line
	statusLine, err := br.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(statusLine, "200") {
		_ = conn.Close()
		t.Fatalf("expected 200, got status line: %q", statusLine)
	}

	// Headers até linha vazia
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			_ = conn.Close()
			t.Fatalf("read headers: %v", err)
		}
		if line == "\r\n" {
			break
		}
	}

	// Espera o primeiro evento "ready" (garante que a tool começou e o stream está ativo)
	deadline := time.Now().Add(1 * time.Second)
	gotReady := false
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data: ") && strings.Contains(line, "ready") {
			gotReady = true
			break
		}
	}
	if !gotReady {
		_ = conn.Close()
		t.Fatalf("did not receive initial SSE data (ready) before disconnect")
	}

	// Simula queda do túnel: fecha conexão do cliente.
	_ = conn.Close()

	// Agora esperamos o subprocesso morrer (marker file aparecer).
	waitDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(waitDeadline) {
		if _, err := os.Stat(marker); err == nil {
			return // PASS
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("tool process did not exit after client disconnect (marker not found)")
}

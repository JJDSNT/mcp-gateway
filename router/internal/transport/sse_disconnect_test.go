package transport_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/core"
	"mcp-router/internal/transport"
)

func TestSSEDisconnect_KillsToolProcess(t *testing.T) {
	marker := t.TempDir() + "/tool_exited.marker"
	t.Setenv("MCP_GW_TEST_TOOL", "1")
	t.Setenv("MCP_TOOL_EXIT_MARKER", marker)

	// Config mínima válida para o runner/runtime
	cfg := &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			"echo": {
				Runtime:   "native",
				Mode:      "launcher",
				Cmd:       os.Args[0],
				Args:      []string{"__mcp_tool_disconnect_helper__"},
				TimeoutMS: 5000,
			},
		},
	}

	svc := core.New(cfg)
	httpT := transport.NewHTTP(svc)

	mux := http.NewServeMux()
	httpT.Register(mux)

	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = false
	srv.Start()
	defer srv.Close()

	// Conexão TCP crua para simular "cliente desconectou" de verdade
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	// POST SSE
	fmt.Fprintf(conn,
		"POST /mcp/echo HTTP/1.1\r\n"+
			"Host: localhost\r\n"+
			"Content-Length: 2\r\n"+
			"Content-Type: application/json\r\n"+
			"\r\n{}")

	br := bufio.NewReader(conn)

	// Consome headers HTTP
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" {
			break
		}
	}

	// Espera a tool escrever "ready" (vai aparecer dentro do SSE como data: ...)
	ready := make(chan struct{}, 1)
	go func() {
		defer func() { _ = recover() }()
		for {
			line, _ := br.ReadString('\n')
			if strings.Contains(line, "ready") {
				ready <- struct{}{}
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		_ = conn.Close()
		t.Fatal("timeout waiting for tool ready")
	}

	// AÇÃO: desconectar cliente
	_ = conn.Close()

	// Espera marker aparecer (prova de que a tool recebeu kill e saiu)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("tool process did not exit (marker file not found at %s)", marker)
}

// (opcional) se você quiser garantir que o servidor encerra mesmo com ctx cancelado:
func TestServerShutdown_DoesNotHang(t *testing.T) {
	cfg := &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			"echo": {
				Runtime:   "native",
				Mode:      "launcher",
				Cmd:       "true",
				Args:      []string{},
				TimeoutMS: 1000,
			},
		},
	}
	svc := core.New(cfg)
	httpT := transport.NewHTTP(svc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = httpT.Run(ctx, ":0")
}

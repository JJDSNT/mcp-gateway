package main

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/runner"
)

func TestMain(m *testing.M) {
	if os.Getenv("MCP_ROUTER_TEST_TOOL") == "1" && len(os.Args) > 1 {
		toolHelperMain()
		return
	}
	os.Exit(m.Run())
}

func toolHelperMain() {
	switch os.Args[1] {
	case "__mcp_tool_helper__":
		fmt.Println("data: first")
		time.Sleep(500 * time.Millisecond)
		fmt.Println("data: second")
		os.Exit(0)

	case "__mcp_tool_disconnect_helper__":
		marker := os.Getenv("MCP_TOOL_EXIT_MARKER")
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

		// Notifica o router que a tool iniciou
		fmt.Println("data: ready")

		// Fica em loop até receber o sinal
		go func() {
			for { time.Sleep(time.Second) }
		}()

		<-sigCh // Espera pelo SIGTERM enviado pelo KillProcess

		if marker != "" {
			// Escreve o marker e força sincronização física com o disco
			f, err := os.Create(marker)
			if err == nil {
				f.WriteString("exited")
				f.Sync()
				f.Close()
			}
		}
		os.Exit(0)
	}
}

func TestSSEDisconnect_KillsToolProcess(t *testing.T) {
	marker := t.TempDir() + "/tool_exited.marker"
	
	cfg = &config.Config{
		Tools: map[string]config.Tool{
			"echo": {
				Runtime: "native", 
				Cmd: os.Args[0], 
				Args: []string{"__mcp_tool_disconnect_helper__"},
			},
		},
	}
	run = runner.New(cfg)
	t.Setenv("MCP_ROUTER_TEST_TOOL", "1")
	t.Setenv("MCP_TOOL_EXIT_MARKER", marker)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(handleMCP))
	srv.EnableHTTP2 = false
	srv.Start()
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	fmt.Fprintf(conn, "POST /mcp/echo HTTP/1.1\r\nHost: localhost\r\nContent-Length: 2\r\nContent-Type: application/json\r\n\r\n{}")

	br := bufio.NewReader(conn)
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" { break }
	}

	ready := make(chan bool, 1)
	go func() {
		for {
			line, _ := br.ReadString('\n')
			if strings.Contains(line, "ready") {
				ready <- true
				return
			}
		}
	}()

	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for tool ready")
	}

	// --- AÇÃO: DESCONECTAR ---
	conn.Close()

	// Espera o marker aparecer por até 5 segundos
	success := false
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(marker); err == nil {
			success = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !success {
		t.Fatalf("tool process did not exit (marker file not found at %s)", marker)
	}
}
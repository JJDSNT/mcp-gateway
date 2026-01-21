package main

import (
	"bufio"
	"context"
	"fmt"
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

// TestMain: permite que o próprio binário de teste funcione como "tool" (processo).
// O runtime native vai executar os.Args[0] com args:
// - "__mcp_tool_helper__" (flush test)
// - "__mcp_tool_disconnect_helper__" (disconnect test)
//
// Como o runtime herda os.Environ(), setamos MCP_ROUTER_TEST_TOOL=1 nos testes.
func TestMain(m *testing.M) {
	if os.Getenv("MCP_ROUTER_TEST_TOOL") == "1" && len(os.Args) > 1 {
		toolHelperMain()
		return
	}
	os.Exit(m.Run())
}

func toolHelperMain() {
	// Decide o modo pelo argv[1]
	switch os.Args[1] {
	case "__mcp_tool_helper__":
		// Emite duas linhas com delay, para provar que o router flushou a primeira
		// antes do processo terminar.
		fmt.Println("first")
		time.Sleep(500 * time.Millisecond)
		fmt.Println("second")
		os.Exit(0)

	case "__mcp_tool_disconnect_helper__":
		// Para o teste, precisamos escrever um marker quando o processo for encerrado.
		// Importante: defer NÃO é confiável em SIGKILL; por isso capturamos SIGTERM/INT.
		marker := os.Getenv("MCP_TOOL_EXIT_MARKER")

		sigCh := make(chan os.Signal, 2)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

		// Emite "ready" e depois fica rodando até ser morto.
		fmt.Println("ready")

		for i := 0; i < 1000000; i++ {
			select {
			case <-sigCh:
				if marker != "" {
					_ = os.WriteFile(marker, []byte("exited"), 0o644)
				}
				os.Exit(0)
			default:
			}

			fmt.Printf("tick-%d\n", i)
			time.Sleep(50 * time.Millisecond)
		}
		os.Exit(0)

	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode:", os.Args[1])
		os.Exit(2)
	}
}

func setTestConfigAndRunnerForSSE(t *testing.T) {
	t.Helper()

	// Tool nativa que executa o próprio binário de teste como helper process.
	// Isso bate com seu NativeRuntime (tool.Cmd, tool.Args...).
	cfg = &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			"echo": {
				Runtime: "native",
				Cmd:     os.Args[0],
				Args:    []string{"__mcp_tool_helper__"},
			},
		},
	}
	run = runner.New(cfg)
}

func TestSSEHeadersAndFlush_StreamIsIncremental(t *testing.T) {
	setTestConfigAndRunnerForSSE(t)

	// O helper process depende dessa env var.
	// Como NativeRuntime herda os.Environ(), o subprocesso vai receber isso.
	t.Setenv("MCP_ROUTER_TEST_TOOL", "1")

	srv := httptest.NewServer(http.HandlerFunc(handleMCP))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/mcp/echo", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	// Headers SSE obrigatórios / esperados
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type: expected %q, got %q", "text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("Cache-Control: expected to contain %q, got %q", "no-cache", cc)
	}
	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Fatalf("Connection: expected %q, got %q", "keep-alive", conn)
	}

	// Esses 2 já existem no seu main.go
	if got := resp.Header.Get("X-MCP-Tool"); got != "echo" {
		t.Fatalf("X-MCP-Tool: expected %q, got %q", "echo", got)
	}
	if got := resp.Header.Get("X-MCP-Runtime"); got != "native" {
		t.Fatalf("X-MCP-Runtime: expected %q, got %q", "native", got)
	}

	// P1: anti-buffering em proxies (nginx/caddy).
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Fatalf("X-Accel-Buffering: expected %q, got %q", "no", got)
	}

	// Provar que o stream chega antes do fim do processo (flush incremental).
	// Queremos observar o "data: first" chegando rápido (antes de 250ms).
	reader := bufio.NewReader(resp.Body)

	type readResult struct {
		line string
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				ch <- readResult{"", err}
				return
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, "first") {
				ch <- readResult{line, nil}
				return
			}
		}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read error before receiving first event: %v", res.err)
		}
		elapsed := time.Since(start)
		if elapsed > 250*time.Millisecond {
			t.Fatalf("stream did not flush incrementally: first event took %s (>250ms)", elapsed)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timeout waiting for first SSE event (stream may be buffered)")
	}
}

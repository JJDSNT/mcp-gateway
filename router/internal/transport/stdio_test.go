package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"mcp-router/internal/config"
	"mcp-router/internal/core"
)

// ----------------------------------------------------------------------
// Tool helper (executa dentro do próprio binário de teste como subprocesso)
// ----------------------------------------------------------------------

func TestMain(m *testing.M) {
	// Quando o runner executa o próprio binário como tool, cai aqui.
	if os.Getenv("MCP_GW_TEST_TOOL") == "1" && len(os.Args) > 1 {
		toolHelperMain()
		return
	}
	os.Exit(m.Run())
}

func toolHelperMain() {
	switch os.Args[1] {

	case "__mcp_tool_echo_helper__":
		// Lê uma linha JSON do stdin e responde com UMA linha JSON no stdout.
		// Compatível com launcher: escreve e sai.
		sc := bufio.NewScanner(os.Stdin)
		if !sc.Scan() {
			// stdin vazio -> responde um json e sai
			fmt.Println(`{"tool":"echo","result":{},"done":true}`)
			os.Exit(0)
		}

		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			fmt.Println(`{"tool":"echo","result":{},"done":true}`)
			os.Exit(0)
		}

		// Não precisa validar muito aqui; é helper de teste.
		var v any
		_ = json.Unmarshal(line, &v)

		out, _ := json.Marshal(map[string]any{
			"tool":   "echo",
			"result": v,
			"done":   true,
		})
		fmt.Println(string(out))
		os.Exit(0)

	case "__mcp_tool_disconnect_helper__":
		marker := os.Getenv("MCP_TOOL_EXIT_MARKER")

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

		// Notifica o router que a tool iniciou (uma linha qualquer no stdout).
		// Pode ser JSON ou texto — o HTTP transport vai embrulhar em SSE.
		fmt.Println(`{"ready":true}`)

		<-sigCh // espera o SIGTERM enviado pelo KillProcess / cancelamento

		if marker != "" {
			// Cria marker no disco pra prova de que o processo saiu
			_ = os.WriteFile(marker, []byte("exited"), 0644)
		}
		os.Exit(0)
	}
}

// ----------------------------------------------------------------------
// Helpers de teste
// ----------------------------------------------------------------------

func newTestCore(t *testing.T) *core.Service {
	t.Helper()

	// Config mínima válida
	cfg := &config.Config{
		WorkspaceRoot: "/tmp/workspaces",
		ToolsRoot:     "/tmp/tools",
		Tools: map[string]config.Tool{
			"echo": {
				Runtime:   "native",
				Mode:      "launcher",
				Cmd:       os.Args[0],
				Args:      []string{"__mcp_tool_echo_helper__"},
				TimeoutMS: 3000,
			},
		},
	}

	return core.New(cfg)
}

type stdioResp struct {
	ID    string          `json:"id,omitempty"`
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

func runStdio(t *testing.T, input string, svc *core.Service) []stdioResp {
	t.Helper()

	// Faz o runner conseguir executar o próprio binário como tool
	t.Setenv("MCP_GW_TEST_TOOL", "1")

	in := bytes.NewBufferString(input)
	out := &bytes.Buffer{}

	tr := NewStdio(svc)
	tr.in = in
	tr.out = out

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := tr.Run(ctx); err != nil {
		t.Fatalf("stdio.Run error: %v", err)
	}

	// parse output (1 JSON por linha)
	sc := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	var resps []stdioResp
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r stdioResp
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("invalid json output line=%q err=%v", string(line), err)
		}
		resps = append(resps, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}
	return resps
}

// ----------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------

func TestStdio_InvalidJSON(t *testing.T) {
	svc := newTestCore(t)

	resps := runStdio(t, "not-json\n", svc)

	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].Event != "error" {
		t.Fatalf("expected event=error, got %q", resps[0].Event)
	}

	var payload map[string]any
	_ = json.Unmarshal(resps[0].Data, &payload)
	if payload["error"] != "invalid_json" {
		t.Fatalf("expected error=invalid_json, got %#v", payload["error"])
	}
}

func TestStdio_MissingTool(t *testing.T) {
	svc := newTestCore(t)

	resps := runStdio(t, `{"id":"1","input":{}}`+"\n", svc)

	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].ID != "1" {
		t.Fatalf("expected id=1, got %q", resps[0].ID)
	}
	if resps[0].Event != "error" {
		t.Fatalf("expected event=error, got %q", resps[0].Event)
	}

	var payload map[string]any
	_ = json.Unmarshal(resps[0].Data, &payload)
	if payload["error"] != "missing_tool" {
		t.Fatalf("expected error=missing_tool, got %#v", payload["error"])
	}
}

func TestStdio_UnknownTool(t *testing.T) {
	svc := newTestCore(t)

	resps := runStdio(t, `{"id":"1","tool":"nope","input":{}}`+"\n", svc)

	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].ID != "1" {
		t.Fatalf("expected id=1, got %q", resps[0].ID)
	}
	if resps[0].Event != "error" {
		t.Fatalf("expected event=error, got %q", resps[0].Event)
	}

	var payload map[string]any
	_ = json.Unmarshal(resps[0].Data, &payload)
	if payload["error"] != "tool_failed" {
		t.Fatalf("expected error=tool_failed, got %#v", payload["error"])
	}
}

func TestStdio_HappyPath_MessageThenDone(t *testing.T) {
	svc := newTestCore(t)

	resps := runStdio(t, `{"id":"abc","tool":"echo","input":{"hello":"world"}}`+"\n", svc)

	if len(resps) < 2 {
		t.Fatalf("expected at least 2 responses (message + done), got %d", len(resps))
	}

	// 1) message
	if resps[0].ID != "abc" {
		t.Fatalf("expected id=abc in first event, got %q", resps[0].ID)
	}
	if resps[0].Event != "message" {
		t.Fatalf("expected first event=message, got %q", resps[0].Event)
	}

	// data do "message" é a linha JSON emitida pela tool
	var toolLine map[string]any
	if err := json.Unmarshal(resps[0].Data, &toolLine); err != nil {
		t.Fatalf("expected message.data to be json from tool, err=%v data=%s", err, string(resps[0].Data))
	}
	if toolLine["tool"] != "echo" {
		t.Fatalf("expected tool=echo, got %#v", toolLine["tool"])
	}

	// confere que carregou hello=world
	result, _ := toolLine["result"].(map[string]any)
	if result["hello"] != "world" {
		t.Fatalf("expected result.hello=world, got %#v", result["hello"])
	}

	// 2) done (último evento deve ser done)
	last := resps[len(resps)-1]
	if last.ID != "abc" {
		t.Fatalf("expected id=abc in last event, got %q", last.ID)
	}
	if last.Event != "done" {
		t.Fatalf("expected last event=done, got %q", last.Event)
	}
}
